# CyberLab Hub Demo Guide

## Before the demo

Requirements: Go 1.26 or later and three terminals in the repository directory. Copy `.env.example` values into the terminal environment, but use a new JWT secret with at least 32 characters. The programs do not load `.env` automatically.

Terminal 1:

```powershell
go run ./cmd/catalog
```

Terminal 2:

```powershell
$env:JWT_SECRET = "replace-with-a-random-secret-at-least-32-characters"
$env:BOOTSTRAP_ADMIN_USERNAME = "operator"
$env:BOOTSTRAP_ADMIN_PASSWORD = "replace-with-a-strong-admin-password"
go run ./cmd/backend
```

Remove the two bootstrap variables after the administrator exists. Terminal 3 and additional terminals:

```powershell
go run ./cmd/tui
```

## 12-minute route

1. HTTP and SQLite: register a learner in the TUI, log in, search for `DNS`, open the challenge, submit a wrong answer, then submit `flag{dns_basics}`. Open My Progress. Restart the backend and show that the account and progress remain.
2. TCP: open a second TUI and log in as the same learner. Keep Notifications visible in TUI B. Solve a previously unsolved challenge in TUI A; TUI B receives the private progress event without polling. A third TUI under another user receives nothing.
3. UDP: log in as the bootstrap administrator in another TUI. Publish a challenge using the admin form. Subscribed learner TUIs display the new-challenge notification; a closed or unsubscribed client does not affect the backend.
4. WebSocket: connect Alice and Bob to `log-analysis`, and Charlie to `forensics`. Send from Alice; Bob receives the message and Charlie does not. Close Bob and send again to show cleanup.
5. gRPC: search from the TUI and explain TUI -> HTTP -> Gin -> gRPC -> catalog SQLite. Stop the catalog and search again; the TUI receives a bounded service error instead of hanging. Restart the catalog.
6. Verification: run `go test -count=1 ./...` and show the protocol-specific tests. Explain that `go test -race ./...` needs a C compiler on this Windows host and is documented as an environment limitation.

## Useful negative cases

- Login with a wrong password.
- Register the same username with different capitalization.
- Submit an empty answer.
- Publish with an invalid challenge ID or difficulty.
- Connect TCP/UDP/WebSocket with an invalid JWT.
- Send a WebSocket message to a different URL room and explain why clients cannot choose a destination in their payload.

## Safe reset

Runtime databases are under `data/` and intentionally ignored by Git. For a fresh demonstration, stop all three programs and move the two database files to a private backup directory. Do not commit demo databases, JWTs, passwords, or `.env` files.
