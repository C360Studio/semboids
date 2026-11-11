package contract

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// repository-governance › "Merge check is its own required workflow" and "One unconditional repository CI
// workflow reports one aggregate" (ateam v0.4.1 layout, C360Studio/ateam#250, #255, #262, and #359). Each assertion
// below has a sensitivity row that mutates the real workflow text and expects the violation.

const (
	ateamPin = "v0.4.1"
	// The job guard in ci.yml: push events run, draft pull requests skip.
	ciDraftGuard = "github.event_name != 'pull_request' || !github.event.pull_request.draft"
	// merge-check.yml has only pull_request events, so its guard is upstream's shorter form.
	mergeCheckDraftGuard = "${{ !github.event.pull_request.draft }}"
	// The merge-check job also skips while the awaiting probe answers awaiting=true (ateam#262). always()
	// keeps it running when the probe fails, so a failed probe means the full check runs.
	mergeCheckGuard = "${{ always() && !github.event.pull_request.draft && needs.awaiting.outputs.awaiting != 'true' }}"
	mergeCheckName  = "${{ github.event.pull_request.draft && 'Merge check (draft, skipped)' || needs.awaiting.outputs.awaiting == 'true' && 'Merge check (awaiting review record, skipped)' || 'Merge check' }}"
	// The Governance job's test-to-spec base: the pull request base, else the previous main commit, else
	// origin/main when a push has no previous commit (an all-zero before).
	testSpecBase = "${{ github.event_name == 'pull_request' && github.event.pull_request.base.sha || (github.event.before != '0000000000000000000000000000000000000000' && github.event.before || 'origin/main') }}"
	probeRun     = `out=$("$ATEAM" review gate --awaiting "$PR_NUMBER")
printf '%s\n' "$out"
printf '%s\n' "$out" | grep -xE 'awaiting=(true|false)' >> "$GITHUB_OUTPUT"`
)

// draftName is the name a job carries so that a skipped draft run is never a check-run with the
// required context's name (GitHub counts a skipped check as passing, ateam#250).
func draftName(name string) string {
	return "${{ github.event.pull_request.draft && '" + name + " (draft, skipped)' || '" + name + "' }}"
}

type gateWorkflow struct {
	On struct {
		PullRequest *struct {
			Types []string `yaml:"types"`
		} `yaml:"pull_request"`
		Push              any `yaml:"push"`
		PullRequestTarget any `yaml:"pull_request_target"`
	} `yaml:"on"`
	Concurrency struct {
		Group            string `yaml:"group"`
		CancelInProgress any    `yaml:"cancel-in-progress"`
	} `yaml:"concurrency"`
	Permissions map[string]string  `yaml:"permissions"`
	Jobs        map[string]gateJob `yaml:"jobs"`
}

type gateJob struct {
	Name        string            `yaml:"name"`
	If          string            `yaml:"if"`
	Needs       needsList         `yaml:"needs"`
	Permissions map[string]string `yaml:"permissions"`
	Outputs     map[string]string `yaml:"outputs"`
	Steps       []gateStep        `yaml:"steps"`
}

// needsList accepts both forms GitHub allows for needs: a single job id or a list.
type needsList []string

func (n *needsList) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		*n = needsList{node.Value}
		return nil
	}
	var list []string
	if err := node.Decode(&list); err != nil {
		return err
	}
	*n = list
	return nil
}

type gateStep struct {
	ID               string            `yaml:"id"`
	Uses             string            `yaml:"uses"`
	Run              string            `yaml:"run"`
	WorkingDirectory string            `yaml:"working-directory"`
	With             map[string]any    `yaml:"with"`
	Env              map[string]string `yaml:"env"`
	If               string            `yaml:"if"`
	ContinueOnError  any               `yaml:"continue-on-error"`
}

func parseGateWorkflow(file string, data []byte) (gateWorkflow, []string) {
	var wf gateWorkflow
	if err := yaml.Unmarshal(data, &wf); err != nil {
		return wf, []string{file + ": parse: " + err.Error()}
	}
	return wf, nil
}

func mergeCheckWorkflowViolations(data []byte) []string {
	const file = "merge-check.yml"
	wf, v := parseGateWorkflow(file, data)
	if v != nil {
		return v
	}
	bad := func(format string, args ...any) { v = append(v, file+": "+fmt.Sprintf(format, args...)) }

	if wf.On.PullRequest == nil {
		bad("no pull_request trigger")
	} else if want := []string{"opened", "synchronize", "reopened", "ready_for_review", "edited"}; !slices.Equal(wf.On.PullRequest.Types, want) {
		bad("pull_request types are %q, want %q (edited is how the reviewer's description edit reruns the gate)", wf.On.PullRequest.Types, want)
	}
	if wf.On.Push != nil || wf.On.PullRequestTarget != nil {
		bad("triggers on push or pull_request_target; want pull_request only")
	}
	if want := "merge-check-${{ github.event.pull_request.number }}"; wf.Concurrency.Group != want {
		bad("concurrency group is %q, want %q", wf.Concurrency.Group, want)
	}
	if wf.Concurrency.CancelInProgress != false {
		bad("concurrency cancel-in-progress is %v, want false (runs for one pull request queue)", wf.Concurrency.CancelInProgress)
	}
	if want := map[string]string{"contents": "read"}; !mapsEqual(wf.Permissions, want) {
		bad("workflow permissions are %v, want %v", wf.Permissions, want)
	}
	if len(wf.Jobs) != 2 {
		bad("has %d jobs, want only awaiting and merge-check", len(wf.Jobs))
	}
	readOnly := map[string]string{"contents": "read", "issues": "read", "pull-requests": "read"}
	probeReadOnly := map[string]string{"contents": "read", "issues": "read", "pull-requests": "read", "checks": "read"}
	install := `GOBIN="$RUNNER_TEMP" go install github.com/c360studio/ateam/cmd/ateam@` + ateamPin
	// Both jobs run ateam from the base commit's checkout with the pinned release, never the tree they judge.
	baseJob := func(id string, job gateJob, wantPerms map[string]string) {
		if !mapsEqual(job.Permissions, wantPerms) {
			bad("%s permissions are %v, want read-only %v", id, job.Permissions, wantPerms)
		}
		if len(job.Steps) == 0 || !strings.HasPrefix(job.Steps[0].Uses, "actions/checkout@") {
			bad("%s does not start with a checkout", id)
		} else {
			with := job.Steps[0].With
			if ref := fmt.Sprint(with["ref"]); ref != "${{ github.event.pull_request.base.sha }}" {
				bad("%s checkout ref is %q, want the base commit", id, ref)
			}
			if with["persist-credentials"] != false {
				bad("%s checkout persist-credentials is %v, want false", id, with["persist-credentials"])
			}
		}
		if !slices.ContainsFunc(job.Steps, func(s gateStep) bool { return strings.TrimSpace(s.Run) == install }) {
			bad("%s has no step that runs %q", id, install)
		}
	}

	if probe, ok := wf.Jobs["awaiting"]; !ok {
		bad("no awaiting job (Merge check probe)")
	} else {
		if probe.Name != "Merge check probe" {
			bad("awaiting name is %q, want %q", probe.Name, "Merge check probe")
		}
		if probe.If != mergeCheckDraftGuard {
			bad("awaiting if is %q, want the draft guard %q", probe.If, mergeCheckDraftGuard)
		}
		baseJob("awaiting", probe, probeReadOnly)
		if !slices.ContainsFunc(probe.Steps, func(s gateStep) bool { return s.ID == "probe" && strings.TrimSpace(s.Run) == probeRun }) {
			bad("awaiting has no step with id probe that runs %q", probeRun)
		}
		if got, want := probe.Outputs["awaiting"], "${{ steps.probe.outputs.awaiting }}"; got != want {
			bad("awaiting output is %q, want %q", got, want)
		}
	}

	job, ok := wf.Jobs["merge-check"]
	if !ok {
		bad("no merge-check job")
		return v
	}
	if !slices.Equal(job.Needs, needsList{"awaiting"}) {
		bad("merge-check needs %q, want [awaiting]", job.Needs)
	}
	if job.Name != mergeCheckName {
		bad("merge-check name is %q, want the draft and awaiting rename %q", job.Name, mergeCheckName)
	}
	if job.If != mergeCheckGuard {
		bad("merge-check if is %q, want the draft and awaiting guard %q", job.If, mergeCheckGuard)
	}
	baseJob("merge-check", job, readOnly)
	var gate *gateStep
	for i := range job.Steps {
		if strings.TrimSpace(job.Steps[i].Run) == `scripts/merge-check.sh "$PR_NUMBER"` {
			gate = &job.Steps[i]
		}
	}
	if gate == nil {
		bad("no step runs scripts/merge-check.sh \"$PR_NUMBER\"")
		return v
	}
	if got, want := gate.Env["ATEAM_REQUIRED_CHECKS"], "Required,Merge check"; got != want {
		bad("ATEAM_REQUIRED_CHECKS is %q, want %q", got, want)
	}
	if got, want := gate.Env["ATEAM"], "${{ runner.temp }}/ateam"; got != want {
		bad("ATEAM is %q, want %q", got, want)
	}
	return v
}

func ciWorkflowGateViolations(data []byte) []string {
	const file = "ci.yml"
	wf, v := parseGateWorkflow(file, data)
	if v != nil {
		return v
	}
	bad := func(format string, args ...any) { v = append(v, file+": "+fmt.Sprintf(format, args...)) }

	if wf.On.PullRequest == nil {
		bad("no pull_request trigger")
	} else if want := []string{"opened", "synchronize", "reopened", "ready_for_review"}; !slices.Equal(wf.On.PullRequest.Types, want) {
		bad("pull_request types are %q, want %q (an edited trigger reruns every lane on a description edit)", wf.On.PullRequest.Types, want)
	}
	lanes := map[string]string{"go": "Go", "ui": "UI", "governance": "Governance"}
	for id := range wf.Jobs {
		if id == "merge-check" {
			bad("merge-check job is still in ci.yml; it belongs to merge-check.yml")
		} else if _, ok := lanes[id]; !ok && id != "required" {
			bad("unexpected job %q", id)
		}
	}
	for id, display := range lanes {
		job, ok := wf.Jobs[id]
		if !ok {
			bad("no %s job", id)
			continue
		}
		if job.Name != draftName(display) {
			bad("%s name is %q, want the draft rename %q", id, job.Name, draftName(display))
		}
		if job.If != ciDraftGuard {
			bad("%s if is %q, want the draft guard %q", id, job.If, ciDraftGuard)
		}
	}
	if gov, ok := wf.Jobs["governance"]; ok {
		// ateam test-spec --base (ateam#237) needs full history for the merge base and the UI's TypeScript for
		// its Vitest scan, and judges the diff against the base this event names.
		if len(gov.Steps) == 0 || !strings.HasPrefix(gov.Steps[0].Uses, "actions/checkout@") || fmt.Sprint(gov.Steps[0].With["fetch-depth"]) != "0" {
			bad("governance does not start with a fetch-depth: 0 checkout")
		}
		if !slices.ContainsFunc(gov.Steps, func(s gateStep) bool { return s.WorkingDirectory == "ui" && strings.TrimSpace(s.Run) == "npm ci" }) {
			bad("governance has no npm ci step in ui")
		}
		if i := slices.IndexFunc(gov.Steps, func(s gateStep) bool { return strings.TrimSpace(s.Run) == "task test:spec" }); i < 0 {
			bad("governance has no step that runs task test:spec")
		} else {
			step := gov.Steps[i]
			if got := step.Env["TEST_SPEC_BASE"]; got != testSpecBase {
				bad("governance test:spec TEST_SPEC_BASE is %q, want %q", got, testSpecBase)
			}
			// A step-level if or continue-on-error would let the gate stop failing Governance.
			if step.If != "" || step.ContinueOnError != nil {
				bad("governance test:spec step must carry no if or continue-on-error")
			}
		}
	}
	req, ok := wf.Jobs["required"]
	if !ok {
		bad("no required job")
		return v
	}
	if req.Name != draftName("Required") {
		bad("required name is %q, want the draft rename %q", req.Name, draftName("Required"))
	}
	if want := "always() && (" + ciDraftGuard + ")"; req.If != want {
		bad("required if is %q, want %q", req.If, want)
	}
	if got, want := slices.Sorted(slices.Values(req.Needs)), []string{"go", "governance", "ui"}; !slices.Equal(got, want) {
		bad("required needs %q, want exactly %q (Required must not depend on Merge check)", req.Needs, want)
	}
	var run strings.Builder
	for _, s := range req.Steps {
		run.WriteString(s.Run)
	}
	// Fail closed: each lane must equal success, so failure, cancelled, skipped and a missing result all fail.
	for id := range lanes {
		if !strings.Contains(run.String(), `"${{ needs.`+id+`.result }}" != "success"`) {
			bad("required step does not fail unless needs.%s.result is success", id)
		}
	}
	if !strings.Contains(run.String(), "exit 1") || strings.Contains(run.String(), "exit 0") {
		bad("required step must exit 1 on a failed lane and never exit 0")
	}
	if strings.Contains(run.String(), "needs.merge-check") {
		bad("required step still reads needs.merge-check")
	}
	return v
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, x := range b {
		if a[k] != x {
			return false
		}
	}
	return true
}

func readWorkflow(t *testing.T, name string) string {
	t.Helper()
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("findRepoRoot: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(repoRoot, ".github", "workflows", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

type workflowMutation struct {
	name        string
	old, repl   string
	wantSubstrs []string
}

func runWorkflowMutations(t *testing.T, base string, check func([]byte) []string, cases []workflowMutation) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if n := strings.Count(base, tc.old); n != 1 {
				t.Fatalf("target %q occurs %d times in workflow", tc.old, n)
			}
			mutated := strings.Replace(base, tc.old, tc.repl, 1)
			requireViolation(t, check([]byte(mutated)), tc.wantSubstrs...)
		})
	}
}

func requireNoViolations(t *testing.T, label string, v []string) {
	t.Helper()
	if len(v) > 0 {
		t.Fatalf("%s violations:\n  - %s", label, strings.Join(v, "\n  - "))
	}
}

func requireViolation(t *testing.T, v []string, wants ...string) {
	t.Helper()
	for _, line := range v {
		ok := true
		for _, w := range wants {
			if !strings.Contains(line, w) {
				ok = false
				break
			}
		}
		if ok {
			return
		}
	}
	t.Fatalf("want a violation containing %q, got:\n  - %s", wants, strings.Join(v, "\n  - "))
}

// spec: openspec/specs/repository-governance/spec.md#Requirement: Merge check is its own required workflow
func TestMergeCheckWorkflow(t *testing.T) {
	text := readWorkflow(t, "merge-check.yml")
	requireNoViolations(t, "merge-check.yml", mergeCheckWorkflowViolations([]byte(text)))

	// Both jobs check out the base commit the same way; these anchors pick one. The probe's checkout follows
	// its steps: key directly, the merge-check job's follows the comment that ends "read through the API.".
	const (
		checkout      = "      - name: Checkout base commit\n        uses: actions/checkout@v5\n        with:\n          ref: ${{ github.event.pull_request.base.sha }}\n          persist-credentials: false\n"
		probeCheckout = "    steps:\n" + checkout
		gateCheckout  = "read through the API.\n" + checkout
	)
	runWorkflowMutations(t, text, mergeCheckWorkflowViolations, []workflowMutation{
		{"edited dropped", "ready_for_review, edited]", "ready_for_review]", []string{"pull_request types"}},
		{"push trigger added", "on:\n", "on:\n  push:\n    branches: [main]\n", []string{"push"}},
		{"concurrency cancels", "cancel-in-progress: false", "cancel-in-progress: true", []string{"cancel-in-progress"}},
		{"concurrency group not per pull request", "group: merge-check-${{ github.event.pull_request.number }}", "group: merge-check", []string{"concurrency group"}},
		{"workflow permission writes", "permissions:\n  contents: read", "permissions:\n  contents: write", []string{"workflow permissions"}},

		// The awaiting probe (ateam#262).
		{"probe dropped", "  awaiting:\n    name: Merge check probe", "  probe-renamed:\n    name: Merge check probe", []string{"no awaiting job"}},
		{"probe renamed", "name: Merge check probe", "name: Merge check", []string{"awaiting name"}},
		{"probe draft guard dropped", "    name: Merge check probe\n    if: " + mergeCheckDraftGuard + "\n", "    name: Merge check probe\n", []string{"awaiting if"}},
		{"probe checks out the head", probeCheckout, strings.Replace(probeCheckout, "base.sha", "head.sha", 1), []string{"awaiting checkout ref"}},
		{"probe checkout dropped", probeCheckout, "    steps:\n", []string{"awaiting does not start with a checkout"}},
		{"probe persists credentials", probeCheckout, strings.Replace(probeCheckout, "persist-credentials: false", "persist-credentials: true", 1), []string{"awaiting checkout persist-credentials"}},
		{"probe permission writes", "      pull-requests: read\n      # The head's Merge check check run (ateam #359).\n      checks: read\n    outputs:", "      pull-requests: write\n      # The head's Merge check check run (ateam #359).\n      checks: read\n    outputs:", []string{"awaiting permissions"}},
		{"probe checks scope dropped", "      # The head's Merge check check run (ateam #359).\n      checks: read\n", "", []string{"awaiting permissions"}},
		{"probe ateam pin drifts", "ateam@" + ateamPin + "\n\n      - name: Is the pull request", "ateam@v0.3.4\n\n      - name: Is the pull request", []string{"awaiting has no step"}},
		{"probe does not ask --awaiting", `review gate --awaiting "$PR_NUMBER"`, `review gate "$PR_NUMBER"`, []string{"id probe"}},
		{"probe output unwired", "awaiting: ${{ steps.probe.outputs.awaiting }}", "awaiting: ${{ steps.probe.outputs.result }}", []string{"awaiting output"}},

		// The merge-check job.
		{"needs awaiting dropped", "    needs: awaiting\n", "", []string{"merge-check needs"}},
		{"awaiting condition dropped", " && needs.awaiting.outputs.awaiting != 'true' }}", " }}", []string{"merge-check if"}},
		{"always dropped", "if: ${{ always() && ", "if: ${{ ", []string{"merge-check if"}},
		{"draft guard dropped", "if: ${{ always() && !github.event.pull_request.draft && ", "if: ${{ always() && ", []string{"merge-check if"}},
		{"awaiting rename dropped", "needs.awaiting.outputs.awaiting == 'true' && 'Merge check (awaiting review record, skipped)' || ", "", []string{"merge-check name"}},
		{"draft rename dropped", "github.event.pull_request.draft && 'Merge check (draft, skipped)' || ", "", []string{"merge-check name"}},
		{"head checked out", gateCheckout, strings.Replace(gateCheckout, "base.sha", "head.sha", 1), []string{"merge-check checkout ref"}},
		{"credentials persisted", gateCheckout, strings.Replace(gateCheckout, "persist-credentials: false", "persist-credentials: true", 1), []string{"merge-check checkout persist-credentials"}},
		{"job permission writes", "      pull-requests: read\n    steps:\n      # The base commit", "      pull-requests: write\n    steps:\n      # The base commit", []string{"merge-check permissions"}},
		{"ateam pin drifts", "ateam@" + ateamPin + "\n\n      # Exit 1", "ateam@v0.3.4\n\n      # Exit 1", []string{"merge-check has no step"}},
		{"Merge check dropped from ATEAM_REQUIRED_CHECKS", "ATEAM_REQUIRED_CHECKS: Required,Merge check", "ATEAM_REQUIRED_CHECKS: Required", []string{"ATEAM_REQUIRED_CHECKS"}},
		{"Required dropped from ATEAM_REQUIRED_CHECKS", "ATEAM_REQUIRED_CHECKS: Required,Merge check", "ATEAM_REQUIRED_CHECKS: Merge check", []string{"ATEAM_REQUIRED_CHECKS"}},
	})
}

// spec: openspec/specs/repository-governance/spec.md#Requirement: One unconditional repository CI workflow reports one aggregate
// spec: openspec/specs/repository-governance/spec.md#Requirement: Repository CI runs the obvious repository checks
func TestCIWorkflowGate(t *testing.T) {
	text := readWorkflow(t, "ci.yml")
	requireNoViolations(t, "ci.yml", ciWorkflowGateViolations([]byte(text)))

	const needs = "needs: [go, ui, governance]"
	runWorkflowMutations(t, text, ciWorkflowGateViolations, []workflowMutation{
		{"required still needs merge-check", needs, "needs: [go, ui, governance, merge-check]", []string{"required needs"}},
		{"required lane dropped", needs, "needs: [go, ui]", []string{"required needs"}},
		{"merge-check job left in ci.yml", "\n  required:\n", "\n  merge-check:\n    name: Merge check\n    runs-on: ubuntu-latest\n    steps:\n      - run: scripts/merge-check.sh\n\n  required:\n", []string{"merge-check job is still in ci.yml"}},
		{"draft guard dropped on required", "if: always() && (" + ciDraftGuard + ")", "if: ${{ always() }}", []string{"required if"}},
		{"rename dropped on required", "name: " + draftName("Required"), "name: Required", []string{"required name", "draft rename"}},
		{"draft guard dropped on go", "name: " + draftName("Go") + "\n    if: " + ciDraftGuard + "\n", "name: " + draftName("Go") + "\n", []string{"go if"}},
		{"rename dropped on governance", "name: " + draftName("Governance"), "name: Governance", []string{"governance name"}},
		{"edited added to ci triggers", "ready_for_review]", "ready_for_review, edited]", []string{"pull_request types"}},
		{"go lane compared to failure only", `"${{ needs.go.result }}" != "success"`, `"${{ needs.go.result }}" == "failure"`, []string{"needs.go.result is success"}},
		{"required step exits 0", "            exit 1\n", "            exit 0\n", []string{"exit 1"}},
		{"governance shallow checkout", "        with:\n          fetch-depth: 0\n", "", []string{"fetch-depth: 0"}},
		{"governance UI install dropped", "        working-directory: ui\n        run: npm ci\n", "        run: echo skipped\n", []string{"npm ci step in ui"}},
		{"test:spec step dropped", "        run: task test:spec\n", "        run: echo skipped\n", []string{"runs task test:spec"}},
		{"test:spec step continue-on-error", "        run: task test:spec\n", "        continue-on-error: true\n        run: task test:spec\n", []string{"no if or continue-on-error"}},
		{"test:spec step if false", "        run: task test:spec\n", "        if: false\n        run: task test:spec\n", []string{"no if or continue-on-error"}},
		{"test:spec base unset", "          TEST_SPEC_BASE: " + testSpecBase + "\n", "", []string{"TEST_SPEC_BASE"}},
		{"test:spec base is head", "github.event.pull_request.base.sha || (github.event.before", "github.event.pull_request.head.sha || (github.event.before", []string{"TEST_SPEC_BASE"}},
		{"test:spec zero-before fallback dropped", "(github.event.before != '0000000000000000000000000000000000000000' && github.event.before || 'origin/main')", "github.event.before", []string{"TEST_SPEC_BASE"}},
	})
}
