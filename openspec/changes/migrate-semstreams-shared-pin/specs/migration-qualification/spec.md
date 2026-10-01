# Migration qualification specification

## ADDED Requirements

### Requirement: Reuse the frozen shared SemStreams pin

The migration SHALL compare SemStreams `v1.0.0-beta.160` at
`8403a2218000e45a31c5132fbfe01af42ed04f14` with SETUP 03A's frozen
`8b99efe9c66a4faa4fa509f9f62cc6bad8392128`, represented as
`v1.0.0-beta.162.0.20260930150212-8b99efe9c66a`. The evidence SHALL record the selecting baseline's
source. A later tag or main commit SHALL NOT silently replace that pin.

#### Scenario: Baseline already selected main

- **GIVEN** SETUP 03A recorded beta.163 absent at selection and froze its main SHA
- **WHEN** SemBoids starts its migration
- **THEN** SemBoids reuses that full SHA and pseudo-version even if a newer tag now exists

### Requirement: Retain SemBoids simulation and substrate boundaries

Seeded physics, spatial hashing, zone semantics, modifier TTLs, population staging and the load dial SHALL
remain SemBoids-owned. Per-tick physics SHALL perform no NATS/KV/rule/graph I/O. Graph snapshots SHALL
remain bounded, non-blocking offers to an off-loop sequential async-batch publisher.

#### Scenario: Publisher is saturated

- **GIVEN** fixed seeded physics and blocked graph publication
- **WHEN** the publisher buffer fills
- **THEN** offered snapshots drop visibly and physics continues without waiting for graph progress

### Requirement: Snapshots preserve lifecycle facts and current neighbor relationships

Snapshot updates SHALL preserve lifecycle-owned phase facts on the same boid entity. Nonempty neighbor
sets SHALL replace prior sets. A nonempty-to-empty transition SHALL clear the declared neighbor group
through the supported typed reconcile contract while preserving unrelated facts and converging indexes.

#### Scenario: Lifecycle and snapshot interleave

- **GIVEN** a boid with active or terminal lifecycle phase and an existing neighbor set
- **WHEN** later position snapshots and neighbor replacements or clears apply
- **THEN** the authoritative entity retains its lifecycle phase and only its current neighbor set
- **AND** incoming/outgoing indexes converge to the same current relationships

### Requirement: Reclamation preserves explicit revision and ambiguity policy

Cull observation SHALL stage physics removal without requiring a browser. Reclamation SHALL read the
exact authoritative revision and delete at that revision, with at most five attempts. Revision conflict
and unavailable permit a fresh-read retry; not-found completes reclamation; commit-unknown SHALL NOT
be blindly retried. Empty-neighbor reconciliation SHALL retain its three-attempt bound and the same
ambiguity discipline. Exhaustion and ambiguity SHALL remain visible through metrics and logs.

#### Scenario: Delete conflicts with an in-flight snapshot

- **GIVEN** a culled boid and a delayed last snapshot write
- **WHEN** deletion encounters a revision conflict
- **THEN** a retry reads a fresh authoritative revision before deleting
- **AND** quiesced graph state and indexes contain no stale relationships to the deleted boid

#### Scenario: Delete outcome is ambiguous

- **GIVEN** a mutation returns commit-unknown
- **WHEN** the reclaimer handles the result
- **THEN** it records the ambiguity and issues no blind retry

### Requirement: Rules and browser delivery remain live

Message-triggered zone rules SHALL preserve enter/exit/linger steering and lifecycle actions. Live
rule toggles SHALL change behavior without replacing components or restarting. Seed/spawn/cull,
clustering, at-most-once frames and graph SSE SHALL remain observable in their corresponding UI panes.

#### Scenario: Disable and re-enable predator behavior

- **GIVEN** a running seeded flock with enabled predator rules
- **WHEN** the steering rule is disabled and later re-enabled through the UI
- **THEN** subsequent zone transitions show the requested behavior without host restart
- **AND** the flock pane continues receiving full frames and the graph pane continues updating

### Requirement: Operational lifecycle has explicit evidence

Fresh startup, failed start, application restart, retained broker restart, cancellation and shutdown SHALL
have recorded outcomes. Start contexts SHALL own runtime work and fresh bounded Stop contexts SHALL
bound shutdown. Controlled Stop SHALL keep the Start context live until Stop completes; canceling Start
first SHALL be tested as the abort path. Submitter goroutines SHALL stop and join before their drain pool
is joined. A timeout SHALL be reported rather than silently counted as a completed join. Application
restart SHALL create a new one-shot component set. No continuing application work SHALL rely on the Stop
context or on Client Close
implicitly stopping child resources. Retained restart SHALL preserve established effective authority.

#### Scenario: Cancel and stop a running host

- **GIVEN** a running host with physics, publishers, watches and transport clients
- **WHEN** cancellation and bounded shutdown occur
- **THEN** owned work stops, submitters join before pool drain, and joins complete or report a specific timeout
- **AND** no further frames or graph publication come from stopped application loops

### Requirement: Comparison measurements preserve workload and accounting meaning

Correctness and performance baseline evidence SHALL be captured before upgrading. Repeated interleaved
baseline/target load runs SHALL use identical seeds, populations, snapshot cadence, churn schedule,
warmup, duration, limits and immutable NATS image, with isolated broker/store per independent run.
Evidence SHALL distinguish offered, acknowledged and applied load, backlog, latency, drops and physics
responsiveness. Each interpreted metric SHALL have verified name, labels, units and increment semantics.

#### Scenario: A counter name overstates its implementation

- **GIVEN** a snapshot counter increments even when its publish batch fails
- **WHEN** migration throughput is reported
- **THEN** the counter is labeled by its actual coordinator-iteration semantics
- **AND** acknowledged and applied work use independently verified evidence

#### Scenario: Scrape or state is unavailable

- **GIVEN** a required metric or authoritative backlog observation is missing
- **WHEN** results are evaluated
- **THEN** the value is unknown and does not become zero, a pass, or a performance improvement

### Requirement: Delivery includes closure, attribution and independent review

The migration SHALL preserve exact production and integration-test dependency closures at both pins,
commands and raw results for build/lint/vet/race/real-NATS and UI/browser gates, and independent review.
Each correctness/performance difference SHALL have evidence-backed attribution or an explicit unresolved
blocker. Baseline failures SHALL remain identifiable and no failed assertion SHALL be silently waived.

#### Scenario: The target fails an existing semantic assertion

- **GIVEN** an assertion passes at the baseline but fails at the shared target
- **WHEN** migration evidence is delivered
- **THEN** the failing test and exact pins are retained with a scoped blocker and attribution evidence
- **AND** the migration remains unqualified until its disposition is explicitly resolved

### Requirement: SemEngine migration requires separate consumer admission

This change SHALL use SemStreams exclusively. A future SemEngine contract SHALL explicitly admit the
required graph, relationship, rule, lifecycle, clustering, transport and host capabilities with owners,
provenance, closure, side effects and proving workload before any SemEngine cutover. Simulation semantics
SHALL remain in SemBoids.

#### Scenario: SemSource's SemEngine workload qualifies

- **GIVEN** SemSource passes its own SemEngine contract
- **WHEN** SemBoids migration readiness is assessed
- **THEN** its additional rule/lifecycle/clustering/browser contracts still require separate qualification
- **AND** SemBoids remains on its frozen SemStreams reference until that work completes
