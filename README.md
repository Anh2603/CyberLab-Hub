# CyberLab Hub

CyberLab Hub is a Go-based cybersecurity learning platform that lets learners discover hands-on challenges, submit answers, track progress, receive release notifications, and discuss topics in real time.

The project is designed to demonstrate five network protocols through connected product features:

- HTTP/JSON for accounts, challenge discovery, submissions, and progress.
- TCP for private progress synchronization between sessions of the same learner.
- UDP for best-effort new-challenge notifications.
- WebSocket for live, room-based discussions.
- gRPC and Protocol Buffers for communication between the backend and a separate Challenge Catalog Service.

## Project status

Phase 1 foundation is implemented. The repository currently provides runnable backend, catalog, and terminal UI entry points, configuration validation, health endpoints, graceful shutdown, and automated tests. Product features are added incrementally according to the roadmap.

## Intended architecture

The system will contain three independently runnable Go programs:

```text
Go tview TUI
  |-- HTTP/JSON ----------> Gin Backend --------> Backend SQLite
  |-- TCP progress sync --> Gin Backend
  |-- UDP notifications --> Gin Backend
  |-- WebSocket chat -----> Gin Backend
                                  |
                                  +-- gRPC --> Challenge Catalog Service
                                                    |
                                                    +--> Catalog SQLite
```

## Core roles

- **Learner:** registers, signs in, browses challenges, submits answers, tracks progress, receives notifications, and joins discussion rooms.
- **Administrator:** imports and reviews challenge content, publishes or archives challenges, and monitors basic service health.
- **Operator:** configures and runs the backend and catalog processes, manages secrets, and backs up local databases.

## Planned challenge categories

- Log analysis
- Network fundamentals
- Digital forensics basics
- Web security fundamentals

All seed challenges and datasets will be local and intentionally created for learning. The project will not execute untrusted user code or deploy intentionally vulnerable public services.

## Technology

- Go 1.25 or later
- Gin
- tview
- SQLite through `database/sql`
- JWT and bcrypt
- Gorilla WebSocket
- grpc-go and Protocol Buffers
- Go `net` package for TCP and UDP

## Documentation

- [Architecture](docs/ARCHITECTURE.md)
- [Roadmap](docs/ROADMAP.md)

## Run the Phase 1 foundation

Requirements: Go 1.26 or later.

Create a local `.env` from `.env.example` and provide a JWT secret containing at least 32 characters. Environment files are intentionally ignored by Git.

Start each service in a separate terminal:

```powershell
$env:JWT_SECRET = "replace-with-a-random-secret-at-least-32-characters"
go run ./cmd/catalog
go run ./cmd/backend
go run ./cmd/tui
```

The backend health endpoint is available at `http://localhost:8080/api/health`. Run the checks with:

```powershell
go test ./...
go vet ./...
go build ./cmd/backend ./cmd/catalog ./cmd/tui
```

## Security principles

- Passwords are hashed and never logged.
- JWT secrets and local environment files are not committed.
- Answer verification stays on the server side.
- User identity is derived from verified authentication, never a client-supplied user ID.
- Network calls use bounded timeouts and cancellation.
- Private progress events are isolated by authenticated user.

## License

No license has been selected yet.
