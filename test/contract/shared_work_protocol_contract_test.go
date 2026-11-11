package contract

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The parts of the agent setup that `ateam doctor --check` (task ateam:doctor) does not hold:
// the six specialist role adapters and any adopter skill adapters.
const (
	maxRoleAdapterLines = 20
	maxSkillLines       = 25
)

type agentRoleSpec struct {
	name         string
	readOnly     bool
	claudeModel  string
	claudeEffort string
	codexModel   string
	codexEffort  string
}

var (
	agentRoleSpecs = []agentRoleSpec{
		{name: "architect", readOnly: true, claudeModel: "opus", claudeEffort: "medium", codexModel: "gpt-6-astra", codexEffort: "medium"},
		{name: "go-developer", readOnly: false, claudeModel: "opus", claudeEffort: "medium", codexModel: "gpt-6-sol", codexEffort: "medium"},
		{name: "go-reviewer", readOnly: true, claudeModel: "opus", claudeEffort: "high", codexModel: "gpt-6-astra", codexEffort: "high"},
		{name: "svelte-developer", readOnly: false, claudeModel: "opus", claudeEffort: "medium", codexModel: "gpt-6-sol", codexEffort: "medium"},
		{name: "svelte-reviewer", readOnly: true, claudeModel: "opus", claudeEffort: "high", codexModel: "gpt-6-astra", codexEffort: "high"},
		{name: "technical-writer", readOnly: false, claudeModel: "sonnet", claudeEffort: "medium", codexModel: "gpt-6-sol", codexEffort: "medium"},
	}
	readOnlyClaudeTools  = []string{"Read", "Bash", "Grep", "Glob", "Skill"}
	writeClaudeTools     = []string{"Read", "Edit", "Write", "Bash", "Grep", "Glob", "Skill"}
	ateamCanonicalSkills = []string{"design", "handoff", "pickup", "ponytail", "preflight", "review-pr"}
)

// spec: openspec/specs/repository-governance/spec.md#Requirement: Repository CI runs the obvious repository checks
func TestAgentSetupParity(t *testing.T) {
	requireNoAgentSetupViolations(t, "agent setup parity", agentSetupViolations("../.."))
}

// spec: openspec/specs/repository-governance/spec.md#Requirement: Repository CI runs the obvious repository checks
func TestAgentSetupParitySensitivity(t *testing.T) {
	base := copyAgentSetupFixture(t, "../..")
	edit := func(rel string, fn func(string) string) map[string]string {
		m := make(map[string]string, len(base))
		for k, v := range base {
			m[k] = v
		}
		if fn == nil {
			delete(m, rel)
		} else {
			m[rel] = fn(m[rel])
		}
		return m
	}
	root := writeAgentSetupTree(t, base)
	requireNoAgentSetupViolations(t, "clean fixture", agentSetupViolations(root))

	for _, tc := range []struct {
		name  string
		files map[string]string
		wants []string
	}{
		{"missing canonical contract", edit(".agents/contracts/architect.md", nil),
			[]string{".agents/contracts/architect.md", "missing"}},
		{"missing codex adapter", edit(".codex/agents/go-reviewer.toml", nil),
			[]string{".codex/agents/go-reviewer.toml", "missing"}},
		{"wrong contract path in claude adapter", edit(".claude/agents/go-reviewer.md", func(s string) string {
			return strings.ReplaceAll(s, "go-reviewer.md", "go-developer.md")
		}), []string{".claude/agents/go-reviewer.md", ".agents/contracts/go-reviewer.md"}},
		{"read-only claude adapter granted Edit", edit(".claude/agents/architect.md", func(s string) string {
			return strings.Replace(s, "tools: Read, Bash, Grep, Glob, Skill", "tools: Read, Edit, Bash, Grep, Glob, Skill", 1)
		}), []string{".claude/agents/architect.md", "tools"}},
		{"claude adapter lists LSP tool", edit(".claude/agents/go-developer.md", func(s string) string {
			return strings.Replace(s, "Skill", "Skill, LSP", 1)
		}), []string{".claude/agents/go-developer.md", "tools"}},
		{"codex read-only adapter missing sandbox_mode", edit(".codex/agents/go-reviewer.toml", func(s string) string {
			return strings.Replace(s, "sandbox_mode = \"read-only\"\n", "", 1)
		}), []string{".codex/agents/go-reviewer.toml", "sandbox_mode"}},
		{"codex write adapter sets sandbox_mode", edit(".codex/agents/go-developer.toml", func(s string) string {
			return "sandbox_mode = \"read-only\"\n" + s
		}), []string{".codex/agents/go-developer.toml", "sandbox_mode"}},
		{"protocol full read mandate", edit(".agents/contracts/go-developer.md", func(s string) string {
			return s + "\nAlways read .agents/protocol.md fully first.\n"
		}), []string{".agents/contracts/go-developer.md", "full read"}},
		{"dangling protocol section pointer", edit(".agents/contracts/go-developer.md", func(s string) string {
			return s + "\nFrom .agents/protocol.md, read § 9.\n"
		}), []string{".agents/contracts/go-developer.md", "dangling § 9"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeAgentSetupTree(t, tc.files)
			requireAgentSetupViolation(t, agentSetupViolations(dir), tc.wants...)
		})
	}
}

func requireNoAgentSetupViolations(t *testing.T, label string, violations []string) {
	t.Helper()
	if len(violations) > 0 {
		t.Fatalf("%s violations:\n  - %s", label, strings.Join(violations, "\n  - "))
	}
}

func requireAgentSetupViolation(t *testing.T, violations []string, wants ...string) {
	t.Helper()
	if len(violations) == 0 {
		t.Fatalf("expected violation containing %v, got none", wants)
	}
	for _, v := range violations {
		allMatch := true
		for _, w := range wants {
			if !strings.Contains(v, w) {
				allMatch = false
				break
			}
		}
		if allMatch {
			return
		}
	}
	t.Fatalf("expected a violation containing all of %v, got:\n  - %s", wants, strings.Join(violations, "\n  - "))
}

func writeAgentSetupTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", full, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}
	return dir
}

func copyAgentSetupFixture(t *testing.T, root string) map[string]string {
	t.Helper()
	rels := []string{
		"AGENTS.md",
		"CLAUDE.md",
		".agents/README.md",
		".agents/protocol.md",
		".agents/rules-index.md",
		".agents/review-policy.json",
		".agents/contracts/developer.md",
		".agents/contracts/reviewer.md",
	}
	for _, r := range agentRoleSpecs {
		rels = append(rels,
			".agents/contracts/"+r.name+".md",
			".claude/agents/"+r.name+".md",
			".codex/agents/"+r.name+".toml",
		)
	}
	for _, name := range ateamCanonicalSkills {
		rels = append(rels,
			".agents/skills/"+name+"/SKILL.md",
			".claude/skills/"+name+"/SKILL.md",
		)
	}
	files := make(map[string]string, len(rels))
	for _, rel := range rels {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		files[rel] = string(data)
	}
	return files
}

func agentSetupViolations(root string) []string {
	var out []string
	out = append(out, checkRequiredAgentFiles(root)...)
	for _, role := range agentRoleSpecs {
		out = append(out, checkClaudeRoleAdapter(root, role)...)
		out = append(out, checkCodexRoleAdapter(root, role)...)
	}
	out = append(out, checkTaskScopedProtocolReads(root)...)
	return out
}

var (
	protocolPointerRe     = regexp.MustCompile(`(skill )?§ (\d+)`)
	sentenceEndRe         = regexp.MustCompile(`[.;:]\s|\n`)
	fullReadRe            = regexp.MustCompile(`(?i)\bread\b.*\b(first|fully)\b`)
	namedProtocolHeadings = []string{"Test-to-spec attribution", "Unattended and fan-out runs"}
)

func readingViolations(protocol, doc string) []string {
	var out []string
	for _, sentence := range sentenceEndRe.Split(doc, -1) {
		if fullReadRe.MatchString(sentence) && strings.Contains(sentence, "protocol.md") && !strings.Contains(sentence, "§") {
			out = append(out, "full read: "+strings.TrimSpace(sentence))
		}
	}
	for _, m := range protocolPointerRe.FindAllStringSubmatch(doc, -1) {
		if m[1] != "" {
			continue
		}
		if !strings.Contains(protocol, "\n## "+m[2]+". ") {
			out = append(out, "dangling § "+m[2])
		}
	}
	for _, name := range namedProtocolHeadings {
		if strings.Contains(doc, "*"+name+"*") && !regexp.MustCompile(`(?m)^#+ `+regexp.QuoteMeta(name)+`$`).MatchString(protocol) {
			out = append(out, "dangling *"+name+"*")
		}
	}
	return out
}

func checkTaskScopedProtocolReads(root string) []string {
	protocolBytes, err := os.ReadFile(filepath.Join(root, ".agents", "protocol.md"))
	if err != nil {
		return nil
	}
	protocol := string(protocolBytes)
	rels := []string{
		".agents/contracts/developer.md",
		".agents/contracts/reviewer.md",
	}
	for _, r := range agentRoleSpecs {
		rels = append(rels, ".agents/contracts/"+r.name+".md")
	}
	for _, name := range ateamCanonicalSkills {
		rels = append(rels, ".agents/skills/"+name+"/SKILL.md")
	}
	var out []string
	for _, rel := range rels {
		doc, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		for _, v := range readingViolations(protocol, string(doc)) {
			out = append(out, fmt.Sprintf("%s: %s", rel, v))
		}
	}
	return out
}

func checkRequiredAgentFiles(root string) []string {
	var out []string
	required := []string{
		"AGENTS.md",
		"CLAUDE.md",
		".agents/README.md",
		".agents/protocol.md",
		".agents/rules-index.md",
		".agents/review-policy.json",
		".agents/contracts/developer.md",
		".agents/contracts/reviewer.md",
	}
	for _, r := range agentRoleSpecs {
		required = append(required,
			".agents/contracts/"+r.name+".md",
			".claude/agents/"+r.name+".md",
			".codex/agents/"+r.name+".toml",
		)
	}
	for _, rel := range required {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil || info.IsDir() || info.Size() == 0 {
			out = append(out, fmt.Sprintf("%s: missing or empty required agent file", rel))
		}
	}
	return out
}

var contractPathPattern = regexp.MustCompile(`\.agents/contracts/[A-Za-z0-9_.-]+\.md`)

func checkClaudeRoleAdapter(root string, role agentRoleSpec) []string {
	rel := ".claude/agents/" + role.name + ".md"
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return nil
	}
	text := string(data)
	var out []string
	out = append(out, checkAdapterCommon(rel, role.name, text)...)

	rest, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		return append(out, fmt.Sprintf("%s: missing YAML frontmatter", rel))
	}
	fmText, _, ok := strings.Cut(rest, "\n---\n")
	if !ok {
		return append(out, fmt.Sprintf("%s: unterminated YAML frontmatter", rel))
	}
	var fm struct {
		Name   string `yaml:"name"`
		Tools  string `yaml:"tools"`
		Model  string `yaml:"model"`
		Effort string `yaml:"effort"`
	}
	if err := yaml.Unmarshal([]byte(fmText), &fm); err != nil {
		return append(out, fmt.Sprintf("%s: invalid frontmatter: %v", rel, err))
	}
	if fm.Name != role.name {
		out = append(out, fmt.Sprintf("%s: frontmatter name = %q, want %q", rel, fm.Name, role.name))
	}
	if fm.Model != role.claudeModel {
		out = append(out, fmt.Sprintf("%s: frontmatter model = %q, want %q", rel, fm.Model, role.claudeModel))
	}
	if fm.Effort != role.claudeEffort {
		out = append(out, fmt.Sprintf("%s: frontmatter effort = %q, want %q", rel, fm.Effort, role.claudeEffort))
	}
	var gotTools []string
	for _, tok := range strings.Split(fm.Tools, ",") {
		if t := strings.TrimSpace(tok); t != "" {
			gotTools = append(gotTools, t)
		}
	}
	wantTools := writeClaudeTools
	if role.readOnly {
		wantTools = readOnlyClaudeTools
	}
	if !slices.Equal(gotTools, wantTools) {
		out = append(out, fmt.Sprintf("%s: frontmatter tools = %v, want %v", rel, gotTools, wantTools))
	}
	return out
}

func checkCodexRoleAdapter(root string, role agentRoleSpec) []string {
	rel := ".codex/agents/" + role.name + ".toml"
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return nil
	}
	text := string(data)
	var out []string
	out = append(out, checkAdapterCommon(rel, role.name, text)...)

	fields, err := parseFlatTOMLStrings(text)
	if err != nil {
		return append(out, fmt.Sprintf("%s: invalid TOML: %v", rel, err))
	}
	if fields["name"] != role.name {
		out = append(out, fmt.Sprintf("%s: name = %q, want %q", rel, fields["name"], role.name))
	}
	if fields["model"] != role.codexModel {
		out = append(out, fmt.Sprintf("%s: model = %q, want %q", rel, fields["model"], role.codexModel))
	}
	if fields["model_reasoning_effort"] != role.codexEffort {
		out = append(out, fmt.Sprintf("%s: model_reasoning_effort = %q, want %q", rel, fields["model_reasoning_effort"], role.codexEffort))
	}
	sandboxVal, hasSandbox := fields["sandbox_mode"]
	if role.readOnly && sandboxVal != "read-only" {
		out = append(out, fmt.Sprintf("%s: read-only role must set sandbox_mode = \"read-only\" (got %q)", rel, sandboxVal))
	}
	if !role.readOnly && hasSandbox {
		out = append(out, fmt.Sprintf("%s: write role must not set sandbox_mode (got %q)", rel, sandboxVal))
	}
	return out
}

func parseFlatTOMLStrings(text string) (map[string]string, error) {
	out := make(map[string]string)
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, rhs, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("line %d: missing '='", i+1)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("line %d: empty key", i+1)
		}
		if _, exists := out[key]; exists {
			return nil, fmt.Errorf("line %d: duplicate key %q", i+1, key)
		}
		rhs = strings.TrimSpace(rhs)
		if strings.HasPrefix(rhs, `"""`) || strings.HasPrefix(rhs, `'''`) {
			delim := rhs[:3]
			rest := rhs[3:]
			if endIdx := strings.Index(rest, delim); endIdx >= 0 {
				out[key] = rest[:endIdx]
				continue
			}
			var buf strings.Builder
			buf.WriteString(rest)
			closed := false
			for i+1 < len(lines) {
				i++
				if endIdx := strings.Index(lines[i], delim); endIdx >= 0 {
					buf.WriteString("\n")
					buf.WriteString(lines[i][:endIdx])
					closed = true
					break
				}
				buf.WriteString("\n")
				buf.WriteString(lines[i])
			}
			if !closed {
				return nil, fmt.Errorf("unterminated multiline string for %q", key)
			}
			out[key] = buf.String()
			continue
		}
		val, err := parseTOMLSingleLineString(rhs)
		if err != nil {
			return nil, fmt.Errorf("line %d (%s): %w", i+1, key, err)
		}
		out[key] = val
	}
	return out, nil
}

func parseTOMLSingleLineString(rhs string) (string, error) {
	if len(rhs) < 2 {
		return "", fmt.Errorf("invalid string literal %q", rhs)
	}
	quote := rhs[0]
	if quote != '"' && quote != '\'' {
		return "", fmt.Errorf("expected quoted string, got %q", rhs)
	}
	escaped := false
	for i := 1; i < len(rhs); i++ {
		ch := rhs[i]
		if quote == '"' && ch == '\\' && !escaped {
			escaped = true
			continue
		}
		if ch == quote && !escaped {
			trailing := strings.TrimSpace(rhs[i+1:])
			if trailing != "" && !strings.HasPrefix(trailing, "#") {
				return "", fmt.Errorf("unexpected trailing tokens %q", trailing)
			}
			return rhs[1:i], nil
		}
		escaped = false
	}
	return "", fmt.Errorf("unterminated string literal %q", rhs)
}

func checkAdapterCommon(rel, roleName, text string) []string {
	var out []string
	wantContract := ".agents/contracts/" + roleName + ".md"
	matches := contractPathPattern.FindAllString(text, -1)
	if len(matches) == 0 || !slices.Equal(matches, []string{wantContract}) {
		out = append(out, fmt.Sprintf("%s: contract references %v, want exactly [%s]", rel, matches, wantContract))
	}
	wantReadFirst := fmt.Sprintf("Your first action is to read `%s` fully", wantContract)
	if !strings.Contains(text, wantReadFirst) {
		out = append(out, fmt.Sprintf("%s: missing %q", rel, wantReadFirst))
	}
	lines := strings.Count(strings.TrimRight(text, "\n"), "\n") + 1
	if lines > maxRoleAdapterLines {
		out = append(out, fmt.Sprintf("%s: %d lines exceeds max %d lines", rel, lines, maxRoleAdapterLines))
	}
	if strings.Contains(text, "- [ ]") || strings.Contains(text, "- [x]") {
		out = append(out, fmt.Sprintf("%s: contains copied checklist checkbox", rel))
	}
	return out
}
