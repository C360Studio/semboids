package contract

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func findRepoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := cwd
	for {
		if info, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil && !info.IsDir() {
			if _, tfErr := os.Stat(filepath.Join(dir, "Taskfile.yml")); tfErr == nil {
				return dir, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("repository root not found from %s", cwd)
		}
		dir = parent
	}
}

// spec: openspec/specs/repository-governance/spec.md#Requirement: Repository CI runs the obvious repository checks
func TestAteamShimResolution(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("findRepoRoot: %v", err)
	}
	shimPath := filepath.Join(repoRoot, "scripts", "ateam")
	shimInfo, err := os.Stat(shimPath)
	if err != nil {
		t.Fatalf("stat scripts/ateam: %v", err)
	}
	if shimInfo.Mode()&0o111 == 0 {
		t.Fatalf("scripts/ateam mode = %v, want executable bit set (0755)", shimInfo.Mode())
	}
	shimBytes, err := os.ReadFile(shimPath)
	if err != nil {
		t.Fatalf("read scripts/ateam: %v", err)
	}

	writeExec := func(t *testing.T, path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	writeFakeAteam := func(t *testing.T, path, tag string) {
		t.Helper()
		writeExec(t, path, "#!/bin/sh\nprintf '%s:%s\\n' '"+tag+"' \"$*\"\n")
	}

	// Build a clean isolated system bin with NO `git` and NO `ateam`, proving
	// the shim resolves both primary checkouts and linked worktrees without forking `git`.
	makeIsolatedPath := func(t *testing.T, extraDirs ...string) string {
		t.Helper()
		sysBin := filepath.Join(t.TempDir(), "sys-bin")
		if err := os.MkdirAll(sysBin, 0o755); err != nil {
			t.Fatalf("mkdir sys-bin: %v", err)
		}
		for _, tool := range []string{"sh", "printf"} {
			if p, lookErr := exec.LookPath(tool); lookErr == nil {
				_ = os.Symlink(p, filepath.Join(sysBin, tool))
			}
		}
		dirs := append([]string{}, extraDirs...)
		dirs = append(dirs, sysBin)
		return strings.Join(dirs, string(os.PathListSeparator))
	}

	setupFixture := func(t *testing.T, shimContent []byte) (mainDir, wtDir, pathBinDir string) {
		t.Helper()
		base := t.TempDir()
		mainDir = filepath.Join(base, "main")
		wtDir = filepath.Join(mainDir, ".worktrees", "feature-x")
		pathBinDir = filepath.Join(base, "path-bin")

		if err := os.MkdirAll(filepath.Join(mainDir, ".git", "worktrees", "feature-x"), 0o755); err != nil {
			t.Fatalf("mkdir main .git: %v", err)
		}
		if err := os.MkdirAll(wtDir, 0o755); err != nil {
			t.Fatalf("mkdir wtDir: %v", err)
		}
		gitFile := "gitdir: " + filepath.Join(mainDir, ".git", "worktrees", "feature-x") + "\n"
		if err := os.WriteFile(filepath.Join(wtDir, ".git"), []byte(gitFile), 0o644); err != nil {
			t.Fatalf("write wt .git: %v", err)
		}
		writeExec(t, filepath.Join(mainDir, "scripts", "ateam"), string(shimContent))
		writeExec(t, filepath.Join(wtDir, "scripts", "ateam"), string(shimContent))
		return mainDir, wtDir, pathBinDir
	}

	runShim := func(dir, pathEnv string, args ...string) (string, int) {
		cmd := exec.Command(filepath.Join(dir, "scripts", "ateam"), args...)
		cmd.Dir = dir
		cmd.Env = []string{"PATH=" + pathEnv}
		out, runErr := cmd.CombinedOutput()
		code := 0
		if runErr != nil {
			if exitErr, ok := runErr.(*exec.ExitError); ok {
				code = exitErr.ExitCode()
			} else {
				code = -1
			}
		}
		return strings.TrimSpace(string(out)), code
	}

	t.Run("worktree_prefers_main_tools_bin_over_worktree_and_path", func(t *testing.T) {
		mainDir, wtDir, pathBinDir := setupFixture(t, shimBytes)
		writeFakeAteam(t, filepath.Join(mainDir, ".tools", "bin", "ateam"), "MAIN")
		writeFakeAteam(t, filepath.Join(wtDir, ".tools", "bin", "ateam"), "WT")
		writeFakeAteam(t, filepath.Join(pathBinDir, "ateam"), "PATH")

		got, code := runShim(wtDir, makeIsolatedPath(t, pathBinDir), "hook", "--event", "session-start")
		if code != 0 || got != "MAIN:hook --event session-start" {
			t.Fatalf("got (%q, exit %d), want (\"MAIN:hook --event session-start\", exit 0)", got, code)
		}
	})

	t.Run("primary_checkout_prefers_main_tools_bin_over_path", func(t *testing.T) {
		mainDir, _, pathBinDir := setupFixture(t, shimBytes)
		writeFakeAteam(t, filepath.Join(mainDir, ".tools", "bin", "ateam"), "MAIN")
		writeFakeAteam(t, filepath.Join(pathBinDir, "ateam"), "PATH")

		got, code := runShim(mainDir, makeIsolatedPath(t, pathBinDir), "version")
		if code != 0 || got != "MAIN:version" {
			t.Fatalf("got (%q, exit %d), want (\"MAIN:version\", exit 0)", got, code)
		}
	})

	t.Run("falls_back_to_worktree_then_path", func(t *testing.T) {
		_, wtDir, pathBinDir := setupFixture(t, shimBytes)
		writeFakeAteam(t, filepath.Join(wtDir, ".tools", "bin", "ateam"), "WT")
		writeFakeAteam(t, filepath.Join(pathBinDir, "ateam"), "PATH")

		got, code := runShim(wtDir, makeIsolatedPath(t, pathBinDir), "mcp")
		if code != 0 || got != "WT:mcp" {
			t.Fatalf("got (%q, exit %d), want (\"WT:mcp\", exit 0)", got, code)
		}

		if err := os.Remove(filepath.Join(wtDir, ".tools", "bin", "ateam")); err != nil {
			t.Fatalf("remove wt ateam: %v", err)
		}
		got, code = runShim(wtDir, makeIsolatedPath(t, pathBinDir), "hook", "--event", "turn-end")
		if code != 0 || got != "PATH:hook --event turn-end" {
			t.Fatalf("got (%q, exit %d), want (\"PATH:hook --event turn-end\", exit 0)", got, code)
		}
	})

	t.Run("missing_everywhere_exits_127_with_install_hint", func(t *testing.T) {
		_, wtDir, _ := setupFixture(t, shimBytes)
		got, code := runShim(wtDir, makeIsolatedPath(t), "version")
		if code != 127 || !strings.Contains(got, "task tools:ateam") {
			t.Fatalf("got (%q, exit %d), want exit 127 mentioning 'task tools:ateam'", got, code)
		}
	})

	t.Run("sensitivity_swapping_search_order_is_detected", func(t *testing.T) {
		mutated := strings.Replace(
			string(shimBytes),
			`for bin in "$main/.tools/bin/ateam" "$here/.tools/bin/ateam"; do`,
			`for bin in "$here/.tools/bin/ateam" "$main/.tools/bin/ateam"; do`,
			1,
		)
		if mutated == string(shimBytes) {
			t.Fatal("search-order mutation did not apply to scripts/ateam")
		}
		mainDir, wtDir, pathBinDir := setupFixture(t, []byte(mutated))
		writeFakeAteam(t, filepath.Join(mainDir, ".tools", "bin", "ateam"), "MAIN")
		writeFakeAteam(t, filepath.Join(wtDir, ".tools", "bin", "ateam"), "WT")
		writeFakeAteam(t, filepath.Join(pathBinDir, "ateam"), "PATH")

		got, code := runShim(wtDir, makeIsolatedPath(t, pathBinDir), "version")
		if code == 0 && got == "MAIN:version" {
			t.Fatal("expected mutated search order to prefer WT over MAIN, proving test sensitivity")
		}
	})

	t.Run("sensitivity_dropping_primary_checkout_else_branch_is_detected", func(t *testing.T) {
		mutated := strings.Replace(
			string(shimBytes),
			"else\n\tmain=$here\nfi",
			"else\n\tmain=/nonexistent\nfi",
			1,
		)
		if mutated == string(shimBytes) {
			t.Fatal("primary-checkout else-branch mutation did not apply to scripts/ateam")
		}
		mainDir, _, pathBinDir := setupFixture(t, []byte(mutated))
		writeFakeAteam(t, filepath.Join(mainDir, ".tools", "bin", "ateam"), "MAIN")
		writeFakeAteam(t, filepath.Join(pathBinDir, "ateam"), "PATH")
		// Remove scripts/ateam's second fallback ($here/.tools/bin/ateam) so primary-checkout $main is tested directly.
		mutatedNoHere := strings.Replace(mutated, ` "$here/.tools/bin/ateam"`, "", 1)
		writeExec(t, filepath.Join(mainDir, "scripts", "ateam"), mutatedNoHere)

		got, code := runShim(mainDir, makeIsolatedPath(t, pathBinDir), "version")
		if code == 0 && got == "MAIN:version" {
			t.Fatal("expected broken primary-checkout main=$here branch to miss MAIN, proving test sensitivity")
		}
	})
}
