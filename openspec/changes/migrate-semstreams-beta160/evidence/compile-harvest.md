# Compile harvest: beta.158 → beta.160

Pinned `github.com/c360studio/semstreams v1.0.0-beta.160` (transitively:
nats.go 1.48→1.52, klauspost/compress 1.18.0→1.18.5, nkeys 0.4.11→0.4.15),
then `go build ./... && go vet ./...`.

## Complete error list (the authoritative work list)

| Site | Error | Fix |
|---|---|---|
| `internal/sim/component.go:78-79` | unknown fields `Type`/`Subject` in `component.PortDefinition` literal (DefaultConfig) | typed `Config: component.NATSPort{Subject: DefaultSubject}` |
| `internal/sim/component.go:181` | `PortDefinition.Subject` undefined (flat field read) | `PortDefinition.Resolve(DirectionOutput)` → `Port.Facts().NATSSubjects()` |
| `internal/sim/component.go:205` | `component.BuildPortFromDefinition` undefined | reuse the resolved `Port` from the same `Resolve` call |
| `cmd/semboids/main.go:431` | unknown field `Name` in `types.ServiceConfig` literal (masked until internal/sim compiled — dependency-failure shadowing) | drop the field; the map key is the sole identity |

## Audit-prediction diff

Exactly the four predicted sites; **no surprise surfaces**. Everything else the
audit cleared compiles untouched: `pkg/graphview` call sites, the rule
processor's runtime-reconfig structural interface, `pkg/lifecycle`
Manager/Workflow, service plane (`RegisterAll`/`NewServiceManager`/
`ConfigureFromServices`/`Dependencies`), `natsclient`
(`PublishBatchToStream`/`GetKeyValueBucket`/`Request`), `componentregistry`
(`RegisterFactory`-based), payload registries.

Note: `go vet` compiles test files and they pass — integration-test port
configs are inline JSON strings, so the retired flat grammar in them is a
runtime parse failure, not a compile error. That is task group 4's work list,
plus the two known runtime-only breaks (dead `triple.remove` responder,
revision-fenced `entity.delete`) which no compiler can surface — task group 3.
