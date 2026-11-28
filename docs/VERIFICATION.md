# Verification entry point

Current application: MangaHub v3.0. See [MangaHub verification](MANGAHUB_VERIFICATION.md)
for the feature table and [TCP queue defect](DEFECT_PROGRESS_QUEUE.md) for the
observed failure, fix and retest.

The previous CyberLab verification record is historical and available in Git.
Its old coverage/vulnerability numbers are not presented as measurements of the
current MangaHub release.

Core checks: `go test -count=1 ./...`, `go vet ./...`, and build all three commands.
Independent-process check: set `MANGAHUB_PROCESS_TEST=1`, then run
`go test -count=1 -run TestMangaProcessRestart -timeout 120s -v ./internal/tui`.

The report is maintained separately and has not been edited by the hardening work.
