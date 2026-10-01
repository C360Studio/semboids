# Hosted CI evidence

The first draft head, `97d24dc082016aa5af1840bb0a52c87021048a18`, passed local ordinary gates but
failed [hosted CI run 36858237083](https://github.com/C360Studio/semboids/actions/runs/36858237083).
Build and lint passed; the test job failed before completing integration tests.

[The retained failure log](first-failure.log) identifies `TestRegisteredPayloadRoundTripAndFloor`.
The new assertion used `reflect.DeepEqual` on triples containing `time.Time`: an equivalent timestamp
decoded in UTC differed in location representation from the fixture's `time.Local`. Local tests ran in
America/Chicago; the runner used UTC. This is a portability defect in the new test, not evidence of a
changed instant or lost payload fact. The correction must compare time instants while retaining exact
checks of every other fact, and explicitly reproduce the UTC case.

Later runs and their exact heads are recorded here separately; this failed attempt is preserved.
