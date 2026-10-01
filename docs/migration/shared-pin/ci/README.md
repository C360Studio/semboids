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

The corrected head, `58d9b7fe4007c65732c9aa6661aad85feb5f8161`, passed
[run 36859818470](https://github.com/C360Studio/semboids/actions/runs/36859818470): build, lint, race unit tests,
real-NATS race integration, and the aggregate status check all succeeded. [The captured job state](corrected-run.json)
records that exact head. Local revalidation under UTC also passed 203 unit and 221 default-parallel integration
test/subtest checks. Neither ordinary CI run invokes the deliberately failing stronger reclamation qualification.

The first failed attempt remains preserved in `first-run.json` and `first-failure.log`.
