# MangaHub

Go/Gin + Go tview application for the IT096IIU MangaHub specification v3.0.
Discover manga metadata, keep a private library, update reading progress, receive
chapter-release notifications, and discuss titles. No manga pages are hosted.

## Prerequisites

- Go 1.26+ with automatic toolchain downloads (`go.mod` selects Go 1.27.4).
- Node.js 22+ on PATH for the supplementary web checks in `verify-demo.ps1`
  (verified here with Node.js 24.14.1). Node.js is not required to run the
  Go backend, catalog, or TUI. No npm install is needed for these checks.
- PowerShell for the provided `.ps1` verification and packaging scripts.
- Three terminals, minimum 110 columns × 35 rows for the TUI.
- Dependencies: `go mod download`. No Redis, Docker, or external metadata API required.

## Run

From the repository directory, terminal 1:

```powershell
go run ./cmd/catalog
```

Terminal 2:

```powershell
$env:JWT_SECRET = [guid]::NewGuid().ToString("N")
go run ./cmd/backend
```

Terminal 3 (repeat for additional sessions):

```powershell
go run ./cmd/tui
```

Create reader accounts in the TUI. There are no preset passwords. For an
administrator, set both variables in the backend terminal **before starting it**:

```powershell
$env:BOOTSTRAP_ADMIN_USERNAME = "operator"
$env:BOOTSTRAP_ADMIN_PASSWORD = Read-Host "Choose a demo administrator password"
```

After the account is created, remove those two environment variables; the account
remains in SQLite. Keep the same JWT secret across restarts to retain existing
sessions; generating a new secret requires signing in again.

## Ports and storage

| Process | Default address | State |
| --- | --- | --- |
| Gin backend: HTTP + WebSocket | `127.0.0.1:8080` | `data/backend.db`: accounts and personal library |
| Backend TCP progress hub | `127.0.0.1:9090` | Authenticated same-user sessions |
| Backend UDP chapter alerts | `127.0.0.1:9091` | Expiring subscriptions |
| Separate gRPC catalog | `127.0.0.1:9092` | `data/catalog.db`: manga metadata |

`.env.example` lists options. Programs do not load `.env` automatically. Set
variables in the relevant PowerShell session. Bindings default to loopback;
gRPC is a trusted internal service without transport authentication.

## Demo and controls

Arrows/Enter select; Tab changes form controls; Esc returns to the menu.
Manga Browser: F2 results, F3 search. Discussion: F2 message, F3 controls, F4 room.

1. Register/login, search `One Piece`, select **One Piece**, and add it to **My Library**.
2. Open a second TUI under the same account and keep **My Library** visible.
   Change the chapter/status in the first. The second updates from a TCP event,
   without periodic polling. A different account does not receive that event.
3. Subscribe in **Notifications** in two TUIs. In an admin TUI choose **Release
   chapter**, enter a new total greater than the catalog's current count, and
   observe UDP alerts. One Piece starts at 1199 chapters in the 2026-11-27 snapshot;
   use 1200 for the first local demo release, or a higher count after repeated demos.
4. Join `general` in two TUIs and `mystery` in another; demonstrate room isolation.
5. Stop catalog, search again, observe a bounded error; restart catalog. Restart
   backend and verify the library persists.

An optional browser interface is available at `http://localhost:8080/login`.
The **required demo uses the Go TUI**; browser views do not replace TCP/UDP evidence.

## Checks

For the Go application checks, only the Go toolchain is needed:

```powershell
go test -count=1 ./...
go vet ./...
go build ./cmd/backend ./cmd/catalog ./cmd/tui
```

For all checks, including the web behavior tests and independent-process restart
test, confirm Node.js 22+ is available and run:

```powershell
node --version
./scripts/verify-demo.ps1 -ProcessRestart
```

Without `-ProcessRestart`, the script runs web checks, Go tests, vet and build
but skips the separate backend/catalog process-restart test. Missing tools or
an unsupported Node.js version stop the script with an explanation; checks are
not silently skipped.

The MangaHub integration tests use real HTTP/TCP/UDP/WebSocket/gRPC listeners,
temporary SQLite databases, and rendered tview simulation screens. See
[assignment checklist](docs/MANGAHUB_ALIGNMENT.md) and
[verification](docs/MANGAHUB_VERIFICATION.md).

The optional race-detector command is
`go test -race -count=1 -run TestManga -timeout 90s ./internal/tui`.
It needs a supported C toolchain; this Windows host currently lacks gcc.
Protobuf validation: `go run github.com/bufbuild/buf/cmd/buf@v1.75.0 lint`.

## Source package

```powershell
./scripts/package-source.ps1
```

This creates `dist/GroupXX_MangaHub_CodeOnly.zip` with source, stubs, tests,
metadata, docs and sample config. It excludes report files, personal databases,
secrets, Git state and caches. Pass `-Group 26` only if 26 is your actual project
group. Add the final report separately before creating the final submission.
The detailed TUI demonstration route is in [DEMO.md](docs/DEMO.md).

## Known limitations and reuse

- The seed contains 30 real manga titles across 14 genres. Metadata is a local
  2026-11-27 snapshot, not a live publishing feed; chapter totals can include
  extras according to the source. See [metadata sources](docs/MANGA_SOURCES.md).
  Normal startup inserts records only into an empty manga table.
- To upgrade the earlier fictional dataset, stop the catalog, back up its database,
  run `go run ./cmd/catalog-refresh`, then restart it. This explicit migration
  retains existing titles/releases and hides known fictional seeds from browsing;
  old library entries remain usable. Repeating it is safe.
- UDP is best-effort; subscriptions renew while active and expire on the server.
- Chat has no persisted history. The MangaHub TUI reconnects broken TCP progress
  sessions with backoff and reloads saved state on successful authentication.
  Keep the same JWT secret over backend restarts; a new secret requires login.
- Redis is optional and is not implemented.
- The repository/module path retains `CyberLab-Hub` for Git/import compatibility.
  Older CyberLab tables, handlers, and regression tests remain for migration
  safety; the three production commands select MangaHub services and screens.
  `docs/REPORT_DRAFT.md` remains an untouched historical draft; current architecture,
  demo, alignment and verification guides describe MangaHub.
- Final `REPORT.pdf` and the pair's contribution/ownership record remain a
  separate submission step. Do not submit personal databases or secrets.
