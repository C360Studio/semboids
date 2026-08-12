# flock-communities delta: shared read-side view subscription

## MODIFIED Requirements

### Requirement: Communities stream to the browser
The boids API service SHALL expose an SSE endpoint
(`GET /boids/graph/stream`) backed by shared read-side view subscriptions over
ENTITY_STATES (boid entities) and COMMUNITY_INDEX, sending each client an
initial full sync captured atomically at a view sequence, and then batched
updates (coalesced per entity, flush interval ~500ms) carrying boid positions,
neighbor lists, and per-entity community assignments.

Each client SHALL be a subscriber to the shared views rather than an
independent bucket watcher. The snapshot and the delta stream that follows it
SHALL be consistent at one sequence: every key applied at or before that
sequence present in the snapshot, every subsequent change delivered on the
stream, with no gap, no duplicate, and no inversion.

The wire format and flush cadence are unchanged.

#### Scenario: Initial sync then increments
- **WHEN** a client connects to the stream
- **THEN** it first receives the current graph state, then periodic batches
  reflecting subsequent KV changes

#### Scenario: Snapshot and stream do not overlap or gap
- **GIVEN** entities are being written continuously while a client connects
- **WHEN** the client receives its initial sync and then its first batches
- **THEN** no entity change is missed between the snapshot and the stream, and
  no change already reflected in the snapshot is redelivered as an increment

#### Scenario: Bounded browser traffic
- **GIVEN** the dial at 30Hz
- **WHEN** updates flood the KV buckets
- **THEN** SSE messages remain bounded by the flush interval (latest state
  per entity per batch), not by the dial rate

## ADDED Requirements

### Requirement: Read-side fan-out is shared, not per client
The service SHALL open at most one bucket watcher per watched bucket for the
lifetime of the process, independent of how many SSE clients are connected, and
SHALL decode and contract-validate each write exactly once regardless of
subscriber count.

Connecting or disconnecting clients SHALL NOT create or destroy bucket watchers.

#### Scenario: Consumer count is flat in client count
- **GIVEN** the service is running with no SSE clients connected
- **WHEN** N clients connect to `GET /boids/graph/stream`
- **THEN** the number of JetStream consumers on ENTITY_STATES and
  COMMUNITY_INDEX is unchanged from the zero-client baseline, for any N

#### Scenario: Disconnect leaves the shared view running
- **GIVEN** several clients are connected and receiving batches
- **WHEN** every client disconnects
- **THEN** the shared views remain running and a subsequently connecting client
  is served its initial sync without a new bucket watcher being opened

### Requirement: A slow client degrades alone
A subscriber that cannot keep pace SHALL have its pending changes coalesced
last-writer-wins per entity rather than accumulating unboundedly, SHALL NOT be
disconnected for slowness, and SHALL NOT block the shared watcher or any other
subscriber.

#### Scenario: Slow client does not stall its peers
- **GIVEN** two clients connected, one consuming the stream slowly
- **WHEN** entity updates flood the buckets
- **THEN** the fast client continues receiving current batches at the flush
  cadence, and the slow client receives fewer batches carrying latest state per
  entity rather than a backlog of superseded ones

### Requirement: Watcher loss ends the stream fail-closed
The service SHALL end the affected SSE responses with an explicit terminal
signal when the shared view loses its bucket watcher and can no longer
guarantee the projection is current, rather than continue serving a frozen
projection, and SHALL re-establish the view when a client next attaches.

#### Scenario: Stale projection is not served silently
- **GIVEN** clients connected and receiving batches
- **WHEN** the shared view loses its watcher
- **THEN** the affected responses end rather than continuing to emit the last
  known state as though it were current

#### Scenario: Recovery needs no reload
- **GIVEN** a stream ended because of watcher loss
- **WHEN** the browser's `EventSource` reconnects automatically
- **THEN** the pane resumes with a fresh initial sync and continues updating,
  without the user reloading the page

### Requirement: Community data remains optional
The stream SHALL serve entity data when COMMUNITY_INDEX does not yet exist,
SHALL NOT fail the connection or the service on its absence, and SHALL begin
carrying community assignments once the bucket appears — without requiring
connected clients to reconnect.

#### Scenario: Stream starts before clustering has ever run
- **GIVEN** COMMUNITY_INDEX does not exist
- **WHEN** a client connects to the stream
- **THEN** it receives boid positions and neighbor lists, with no community
  assignments, and the connection is healthy

#### Scenario: Assignments appear mid-connection
- **GIVEN** a client connected while COMMUNITY_INDEX did not exist
- **WHEN** clustering later produces the bucket and its first assignments
- **THEN** subsequent batches to that same connection carry community
  assignments
