# Frozen SemBoids baseline

Captured before upgrading the dependency or changing implementation on 2026-10-01 (America/Chicago).
The source was extracted with `git archive` into `/tmp/semboids-shared-pin/baseline` and left unchanged.

## Identity and environment

- SemBoids source: `8c03cc53836ced93a5df7064473c63ff144e64f1`.
- SemStreams: `v1.0.0-beta.160`, full upstream commit `8403a2218000e45a31c5132fbfe01af42ed04f14`.
- Module zip: `h1:sVkaLPOqs0Jy/iyYvfamkSi9An+Vq0o/5nqMxZVCvtk=`.
- Module manifest: `h1:hrXy53pvnlwI2cg4BlOmXNJtXRKZ9WlrM5ZHmZPn2d8=`.
- Go: `go1.26.4 darwin/arm64`; project go directive: `1.26.3`; CGO enabled for native race tests.
- Go build cache: `/tmp/semboids-shared-pin/go-build-cache` (default host cache is sandbox read-only).
- Real-NATS tests used isolated testcontainers, serial Go packages (`-p=1`).
- Observed test-helper image: `nats:2.14-alpine`, digest
  `sha256:4063edae0717ba5f7501bfde75f97fd9b57f5b93597b92c70b6a6fbbf6a74e06`.
  CI pre-pulls `nats:2.12-alpine`; that pre-pull does not override the SemStreams helper's actual image.

## Gates

| Gate | Result | Evidence |
|---|---|---|
| Native backend build | PASS | `build-native.log` |
| `go vet ./...` | PASS | `vet.log` |
| `go vet -tags=integration ./...` | PASS | `vet-integration.log` |
| `gofmt -l` over Go sources | PASS, empty output | `fmt.log` |
| Revive with project configuration | PASS, empty output | `revive.log` |
| Uncached race unit suite | PASS, 175 test/subtest passes, no skips | `race-unit.log` |
| Uncached race real-NATS integration suite | FAIL, 191 passes, one failure, no skips | `race-integration.log` |
| Linux/amd64 cross-compile, CGO disabled | PASS | `linux-build.log` |
| Schema generation drift | NOT APPLICABLE | CI explicitly defers schema validation until a schema exists |

The first native build attempt could not write the default Go cache. Re-running with the writable cache succeeded;
this is an environment restriction, not a source build failure. Commands, durations, and exit codes are retained in
`gate-status.json` and `integration-status.json`. Browser/UI and measured load evidence are captured separately.

## Pre-existing correctness failure

`TestPredatorCullReclaimsBoid` reaches its behavioral condition (physics 12 → 3 boids; three boid entities remain),
then fails the race detector during cleanup. `Component.Stop` starts `drainPool.wait()` while `runCullWatcher` can
still submit work. The WaitGroup wait/add race is present on the frozen beta.160 source, before this migration.
The complete stack is retained in `race-integration.log`, lines 773–889. The target must join lifecycle submitters
before waiting for the drain pool, and must report incomplete cancellation or shutdown instead of hiding it.

The same fixture emits rule-event validation errors because its minimal rule has no display `name`. The lifecycle
transition still occurs. This is baseline evidence; a future fixture correction must be identified separately
from changes in SemStreams behavior. No baseline failure was retried away or reported as a pass.

Existing cull integration runs with snapshot cadence zero and checks entity count only. It does not establish
that snapshots preserve lifecycle facts, nor that surviving boids retract relationships to a deleted neighbor.
Those require migration regression tests. SemStreams deliberately preserves assertions owned by a live source
when a relationship target is deleted; therefore surviving-source replacement or empty-neighbor reconciliation
must complete before asserting absence of stale graph relationships.

## Dependency closure

`modules.tsv` records all 298 selected modules (including the root), selected version, directness, checksums where
reported by Go, and reachability in package closures. `module-graph.txt` records every selected dependency edge.
`production-packages.tsv` captures 517 packages reached from `./cmd/semboids` on darwin/arm64, including standard
library packages and 60 SemStreams packages. `integration-test-packages.tsv` captures 537 packages reached from
`go list -deps -test -tags=integration ./...`. Both closures reach 67 external modules.

These are package dependency closures, not a claim that every symbol survives linker elimination. In particular,
SemStreams `natsclient/test_client.go` is ordinary package source and imports testcontainers, so testcontainers and
Docker dependencies appear even in the production package closure. Service/registry imports also bring agentic
and gated-DAG packages into this closure. `direct-semstreams-imports.txt` records application import sites.

## Reproduction

```sh
mkdir -p /tmp/semboids-baseline
git archive 8c03cc53836ced93a5df7064473c63ff144e64f1 | tar -x -C /tmp/semboids-baseline
cd /tmp/semboids-baseline
export GOCACHE=/tmp/semboids-go-build-cache
go build -o /tmp/baseline-semboids ./cmd/semboids
go vet ./...
go vet -tags=integration ./...
gofmt -l $(rg --files cmd componentregistry internal -g '*.go')
go tool revive -config revive.toml -formatter friendly -exclude ui/... ./...
go test -count=1 -v -race ./...
go test -count=1 -p=1 -tags=integration -v -race ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags='-w -s' -o /tmp/baseline-linux ./cmd/semboids
go list -m -json all
go mod graph
go list -deps -json ./cmd/semboids
go list -deps -test -tags=integration -json ./...
```

Docker must be available for the integration command.
`SHA256SUMS.json` identifies the retained evidence artifacts. Raw Go closure JSON is additionally available in
`/tmp/semboids-shared-pin/baseline-evidence` during this session; the TSVs preserve the relevant identity fields.
