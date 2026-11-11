## 1. Approval

- [x] 1.1 Owner requested and approved `adopt-ateam-v041` in Enjoy conversation `Migrate to ateam v0.4.1` (`#14`), specifying the `semteams` pattern and `ateam v0.4.1`.

## 2. Harness, Shim, and Workflows

- [x] 2.1 Add `.agents/ateam.json` (`v0.4.1`), `scripts/ateam`, `scripts/doctor.sh`, `scripts/merge-check.sh`, `scripts/validate.sh`, `scripts/verify.sh`, `Taskfile.yml`, and `taskfiles/openspec.yml`.
- [x] 2.2 Consolidate `CLAUDE.md` into `@AGENTS.md` import, add numbered rule sections to `AGENTS.md` and `.agents/rules-index.md`, and install `.agents/` contracts, skills, `.agents/review-policy.json`, and `.agents/hosts/enjoy.toml`.
- [x] 2.3 Replace `.github/workflows/ui.yml` and `.github/workflows/ci.yml` with unified `Repository CI` (`go`, `ui`, `governance`, `required`) and `.github/workflows/merge-check.yml`, and add contract tests in `test/contract/`.
