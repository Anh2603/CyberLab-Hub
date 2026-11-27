# MangaHub

Go/Gin + Go tview application for the IT096IIU MangaHub specification v3.0.
Discover manga metadata, keep a private library, update reading progress, receive
chapter-release notifications, and discuss titles. No manga pages are hosted.

## Prerequisites

- Go 1.26+ with automatic toolchain downloads (`go.mod` selects Go 1.27.4).
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

1. Register/login, search `Star`, select **Star Harbor**, and add it to **My Library**.
2. Open a second TUI under the same account and keep **My Library** visible.
   Change the chapter/status in the first. The second updates from a TCP event,
   without periodic polling. A different account does not receive that event.
3. Subscribe in **Notifications** in two TUIs. In an admin TUI choose **Release
   chapter**, enter a new total greater than the catalog's current count, and
   observe UDP alerts. Star Harbor starts at 12 chapters in an empty catalog.
4. Join `general` in two TUIs and `mystery` in another; demonstrate room isolation.
5. Stop catalog, search again, observe a bounded error; restart catalog. Restart
   backend and verify the library persists.

An optional browser interface is available at `http://localhost:8080/login`.
The **required demo uses the Go TUI**; browser views do not replace TCP/UDP evidence.

## Checks

```powershell
go test -count=1 ./...
go vet ./...
go test -race -count=1 -run TestManga -timeout 90s ./internal/tui
go build ./cmd/backend ./cmd/catalog ./cmd/tui
```

The MangaHub integration tests use real HTTP/TCP/UDP/WebSocket/gRPC listeners,
temporary SQLite databases, and rendered tview simulation screens. See
[assignment checklist](docs/MANGAHUB_ALIGNMENT.md) and
[verification](docs/MANGAHUB_VERIFICATION.md).

## Known limitations and reuse

- The 30 seeded titles are original fictional classroom metadata across six
  genres, not a current commercial catalog. They are inserted only into an empty
  manga table; restarts preserve chapter releases.
- UDP is best-effort; subscriptions renew while active and expire on the server.
- Chat has no persisted history. Reopen the TUI after a broken TCP connection;
  reconnecting reloads authoritative progress from HTTP/SQLite.
- Redis is optional and is not implemented.
- The repository/module path retains `CyberLab-Hub` for Git/import compatibility.
  Older CyberLab tables, handlers, and regression tests remain for migration
  safety; the three production commands select MangaHub services and screens.
  Old `docs/REPORT_DRAFT.md` and other CyberLab documents are historical drafts.
- Final `REPORT.pdf` and the pair's contribution/ownership record remain a
  separate submission step. Do not submit personal databases or secrets.
