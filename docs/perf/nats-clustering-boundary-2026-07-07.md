# NATS Clustering Boundary — why scale-out doesn't move our melt

A recurring question: SemStreams has NATS clustering on the roadmap — should
semboids suggest users run a cluster, given how hard we hammer the substrate?
For the load shape semboids actually produces, **no** — and this note records
why, so it isn't re-litigated.

## The bottleneck is a single hot KV bucket's write path

Everything semboids melts funnels into **one** KV bucket, `ENTITY_STATES`
(graph-ingest is its sole writer — ADR-001). A NATS KV bucket is one JetStream
stream = one RAFT group = **one leader node** that serializes every write. Each
mutation is a CAS (read revision → revision-checked put) against that leader.

The parallel-drain A/B (`churn-lifecycle-2026-07-06.md` addendum) pinned the
ceiling to that path: create throughput was **identical at 8 and 16** app-side
concurrency (1,424 vs 1,426/s) — the wall is the shared NATS connection + the
single KV write path, not the app. beta.142's melt addendum diagnosed the same
sublinear ceiling for graph-ingest's own 8 lanes.

## Clustering is the wrong axis for a single hot partition

Clustering is **scale-out**; this is a **single-hot-partition** problem. They
don't meet:

- **Adding nodes does not shard one bucket.** All `ENTITY_STATES` writes still
  route to its single leader, whichever node a client connects to. Extra nodes
  sit idle for this workload.
- **Replication makes it worse.** R3/R5 means every CAS put waits on RAFT
  quorum acks + multi-node fsync — added round-trips on a path that is *already*
  round-trip-latency bound (the box is ~92% CPU-idle; it's waiting on I/O).
- **HA/durability isn't the goal.** semboids is a load generator — we want to
  melt the substrate, not survive a node loss.

Status: this is an **architecture analysis grounded in the measured single-node
ceiling**, not a measured single-vs-cluster A/B. The prediction (flat, or a dip
under R3) follows from NATS KV's single-leader model; running the actual A/B is
proposed below.

## Where clustering *would* pay off (the real scale-out path)

Shard `ENTITY_STATES` into N buckets by entity-ID hash → N streams → N leaders →
*then* a cluster places those leaders on different nodes and aggregate write
throughput scales with node count. **Sharded buckets + cluster** is the genuine
"add a node, watch the ceiling climb" story — and semboids would be an ideal
vehicle for it (crank churn, add a node, watch `boids_lifecycle_spawns_total`/s
rise). But the sharding is a substrate change and comes first; clustering alone
buys nothing without it.

## Recommendation

- **Do not** suggest "run a cluster" as a semboids throughput tip. For a
  single-bucket write melt the fix is scale-**up** — batch the round-trips
  (semstreams#498) and shorten the write path (#480) — not scale-**out**.
- **Roadmap:** pair clustering with entity-bucket sharding; only together do
  they scale this workload.
- **Candidate demo:** a single-node vs 3-node-cluster A/B on the churn dial to
  *prove* the ceiling doesn't move (and dips under R3) — a concrete "here's the
  boundary of where clustering helps" teaching artifact for SemStreams users,
  worth more than a vague "clustering makes it faster."
