# Verification entry point

Current application: MangaHub v3.0. See [MangaHub verification](MANGAHUB_VERIFICATION.md)
for the feature table and [TCP queue defect](DEFECT_PROGRESS_QUEUE.md) for the
observed failure, fix and retest.

Measurements in the linked verification record apply to the MangaHub runtime
and its connected protocol workflows.

Core checks: `go test -count=1 ./...`, `go vet ./...`, and build all three commands.
Independent-process check: set `MANGAHUB_PROCESS_TEST=1`, then run
`go test -count=1 -run TestMangaProcessRestart -timeout 120s -v ./internal/tui`.

The report is maintained separately and has not been edited by the hardening work.
