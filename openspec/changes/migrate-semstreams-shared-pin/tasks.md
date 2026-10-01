# Migration tasks

## 1. Architecture and baseline handoff

- [x] 1.1 Architect: read project constraints and locate frozen SETUP 03A pin with full SHA and source.
- [x] 1.2 Architect: inventory direct contracts, compatibility risks and future SemEngine admission obligations.
- [x] 1.3 Architect: approve retained contract and failing-first proving obligations before adaptation.
- [x] 1.4 Program manager: open and attach draft PR with exact before/target pins and incomplete gates.
- [x] 1.5 Go developer: preserve baseline source/binary/config; capture dependency and test-only closure.
- [x] 1.6 Go developer: run baseline correctness, operational lifecycle, real-NATS and browser evidence.
- [x] 1.7 Measurement owner: audit metric names/increment semantics and capture baseline load before upgrade.

## 2. Compatibility implementation

- [x] 2.1 Go developer: record target compile/config failures without selecting a different revision.
- [x] 2.2 Go developer: add failing tests for changed context ownership, factory injection and effective authority.
- [x] 2.3 Go developer: adapt host/config/registrations/control seams, preserving live rule activation.
- [x] 2.4 Go developer: prove seeded physics and bounded sequential batch publication unchanged.
- [x] 2.5 Go developer: prove snapshot/lifecycle coexistence, neighbor replacement and empty clearing.
- [ ] 2.6 Go developer: prove seed/spawn/cull, explicit reclaim retries, no stale relationships/resurrection.
- [x] 2.7 Go developer: prove graph-index/clustering and WebSocket/SSE payload continuity.
- [x] 2.8 Go reviewer: independent correctness, concurrency, context, API and TDD review with formal disposition.
- [x] 2.9 Svelte reviewer: verify flock and graph panes, live controls, reconnect and error presentation.

## 3. Qualification and attribution

- [x] 3.1 Go developer: run build/cross-compile, fmt/revive, vet, race and real-NATS integration gates.
- [x] 3.2 Frontend owner: run UI lint, svelte-check, vitest and production build; verify both panes in browser.
- [x] 3.3 Go developer: test fresh startup, application restart, broker restart, cancellation and shutdown.
- [x] 3.4 Measurement owner: repeat interleaved baseline/target runs with identical workload and isolated brokers.
- [x] 3.5 Measurement owner: record offered/published/processed, pending/ACK-pending, latency, drops and physics.
- [x] 3.6 Architect: attribute every observed difference or explicitly record unresolved cause and blocker.
- [x] 3.7 Go reviewer: independently review exact pins, closure, raw evidence and reproducibility claims.

## 4. Documentation and delivery

- [x] 4.1 Technical writer: publish exact pins, compatibility changes, closure and reproducible results.
- [x] 4.2 Technical writer: record blockers without changing frozen failures or relaxing retained semantics.
- [x] 4.3 Architect: provide future SemEngine admission matrix without implying cutover qualification.
- [x] 4.4 Program manager: update draft PR around actual final scope and independent review disposition.
- [x] 4.5 Program manager: preserve the open proposal while qualification remains on HOLD; do not archive it.

## Qualification holds

- Task 2.6 remains incomplete: delayed snapshots recreate reclaimed boids on both pins (revision 1 → 3).
  The explicit failing `integration,qualification` test and logs remain; upstream #1444 tracks the missing barrier.
- Task 3.3 records exercised scenarios, not universal success: the target's first cold start at 30Hz graph
  cadence failed when early simulator requests opened the shared circuit before graph providers finished startup.
  Upstream #1447 tracks the supported provider-readiness contract. The separate matched zero-load boot/dial-activation
  comparison cannot qualify that startup boundary.
- Tasks 2.8–2.9 record completed review/browser work, not migration admission. Overall independent disposition is HOLD.
- The IPv4-only occupied-port attempt is invalid evidence. Corrected dual-family proof shows baseline
  remains running after a bind error; target exits with status 1 in approximately 175 ms.
- No physics algorithm or UI source changed. UI checks passed before adaptation; final target browser checks passed.
- Hosted CI exposed a UTC-dependent assertion in a new payload test. Its test-only correction passes all ordinary
  gates under UTC, including the uncached default-parallel real-NATS race suite; the failed attempt remains recorded.
- All eight load runs completed. Stable pair direction reverses, so no speedup/regression is established.
  Churn uses identical input settings but actual culls differ (0/1/0/6); population traces bound interpretation.
