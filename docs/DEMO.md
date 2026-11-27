# MangaHub live demo — 12–15 minutes

Both students should perform and explain these steps using the Go tview TUI.
The web interface is supplementary and does not replace TCP/UDP TUI evidence.

## Setup

Open terminals in the repository folder. Start catalog:

```powershell
go run ./cmd/catalog
```

Start backend in another terminal. Keep this terminal and its secret for restarts:

```powershell
$env:JWT_SECRET = [guid]::NewGuid().ToString("N")
$env:BOOTSTRAP_ADMIN_USERNAME = "operator"
$env:BOOTSTRAP_ADMIN_PASSWORD = Read-Host "Choose the local demo admin password"
go run ./cmd/backend
```

The bootstrap creates that account once and does not replace an existing reader.
After creation, remove the two bootstrap variables from the backend terminal.
Do not regenerate JWT_SECRET when restarting during the demo.

Open additional TUI terminals, each running:

```powershell
go run ./cmd/tui
```

Use two sessions of one reader account, one different reader, and an admin session.
Create readers through Sign up. Minimum terminal size: 110×35. Arrows/Enter select,
Tab moves through forms, Esc returns to menu. Browser F2 results/F3 search;
Discussion F2 message/F3 controls/F4 room.

## Live route

| Time | Action | Evidence to show |
| --- | --- | --- |
| 0–2 min | Register/login; search Naruto; open details; add to library | HTTP + JWT, 30-title catalog, real Gin→gRPC request; duplicate add gives a controlled error |
| 2–4 min | Open My Library in second session of same user; edit chapter/status in first | Second TUI changes without refresh; version increases; different user's library stays private |
| 4–5 min | Enter chapter above the manga total | Rejected; database/version does not change |
| 5–7 min | Subscribe to Notifications in two TUIs; keep a third unsubscribed; admin releases new One Piece total | gRPC saves chapter; two TUI notification widgets show UDP alert; third does not |
| 7–9 min | Join general in two TUIs and mystery in a third; send both ways; close one client | Same-room chat arrives live, other room isolated; remaining clients keep working |
| 9–11 min | Stop catalog; search; restart catalog | Bounded error instead of hang; search recovers after restart |
| 11–13 min | Stop/restart backend in same terminal; check library and edit again | Saved chapter/version survives; second TUI reconnects and restores current progress |
| 13–15 min | Explain a code path and respond to a small modification | Each student owns a networking decision and failure case |

One Piece's initial snapshot total is 1199. Release 1200 on the first local demo,
or inspect the current total and use a higher number on later runs. Local demo
releases are not claims about actual publisher releases.

## Reproducible verification commands

```powershell
go test -count=1 ./...
go vet ./...
go test -count=1 -run 'TestProgress|TestMangaTerminalBurst' ./internal/tui
$env:MANGAHUB_PROCESS_TEST = "1"
go test -count=1 -run TestMangaProcessRestart -timeout 120s -v ./internal/tui
Remove-Item Env:MANGAHUB_PROCESS_TEST
```

The opt-in restart test builds real backend/catalog executables, uses independent
processes, temporary ports/databases and a rendered TUI. It does not stop the
user's normal services. See [defect evidence](DEFECT_PROGRESS_QUEUE.md).

## Questions to prepare

- Why does TCP need newline framing? Which user/session receives its event?
- Why is the database committed before progress is pushed?
- What can UDP lose, and why does an unsubscribe matter?
- Which goroutine owns WebSocket writes and room membership?
- Why does stopping catalog affect search but not erase a saved library?
- Why is web alone insufficient for the five-protocol demonstration?

The final English report and both students' live attendance remain required by
the assignment; this guide does not claim those activities have been completed.
