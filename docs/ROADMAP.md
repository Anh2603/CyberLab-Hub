# Development Roadmap

The roadmap is organized around working vertical slices. A phase is complete only when its relevant build and tests pass.

## Phase 0 - Product and architecture

- Confirm learner, administrator, and operator workflows.
- Freeze protocol responsibilities.
- Define data ownership and security boundaries.
- Record explicit out-of-scope features.

**Exit:** architecture is reviewable and no protocol is included only as a standalone demo.

## Phase 1 - Module and service foundations

- Create the Go module and three commands: TUI, backend, and catalog.
- Add environment-based configuration and validation.
- Add graceful startup and shutdown.
- Add health checks and structured logging without credentials.

**Exit:** all three programs build and start with documented configuration.

## Phase 2 - Storage and authentication

- Create backend and catalog migrations.
- Implement registration, login, bcrypt hashing, and JWT validation.
- Add role-based authorization for administrator endpoints.
- Seed an initial administrator through a safe local setup path.

**Exit:** account persistence, wrong-password, duplicate-user, invalid-token, and authorization tests pass.

## Phase 3 - Challenge catalog over gRPC

- Define Protocol Buffer messages and generated Go stubs.
- Implement search, details, answer verification, publish, and archive RPCs.
- Seed synthetic challenges only when the catalog is empty.
- Map validation and storage failures to appropriate gRPC status codes.

**Exit:** the catalog runs independently and its RPC contract has automated tests.

## Phase 4 - HTTP challenge workflow

- Connect Gin to the catalog using bounded gRPC deadlines.
- Implement challenge search, details, submission, and learner progress endpoints.
- Keep answer verification material outside public responses.
- Map stopped-catalog errors to controlled HTTP responses.

**Exit:** login-to-solve works end to end and the catalog failure is bounded.

## Phase 5 - TCP progress synchronization

- Authenticate the first newline-delimited TCP message.
- Support multiple sessions for one learner.
- Publish progress only after the database transaction commits.
- Exclude the originating session and all other users.
- Clean up disconnected sessions.

**Exit:** real receiver tests prove same-user delivery and cross-user isolation.

## Phase 6 - UDP publication notifications

- Implement authenticated subscribe and unsubscribe datagrams.
- Track subscriber return addresses with expiration.
- Notify subscribers only after successful catalog publication.
- Treat delivery as best-effort.

**Exit:** subscribed clients receive notifications, unsubscribed clients do not, and unreachable receivers do not crash the backend.

## Phase 7 - WebSocket discussion rooms

- Authenticate the upgrade.
- Implement server-controlled room membership.
- Add safe register, broadcast, and unregister behavior.
- Bound message sizes and write times.

**Exit:** same-room delivery, room isolation, and disconnect cleanup tests pass.

## Phase 8 - Complete TUI

- Implement keyboard-driven screens and visible status feedback.
- Keep HTTP and network listeners off the UI event loop.
- Route incoming events through `QueueUpdateDraw`.
- Support multiple simultaneous TUI processes.

**Exit:** all core workflows are usable without shell commands or manual refresh.

## Phase 9 - Verification and documentation

- Run `gofmt`, `go vet ./...`, `go test -count=1 ./...`, and `go test -race ./...` where supported.
- Add persistence, cancellation, concurrent-client, and failure-injection tests.
- Document setup, demo route, observed results, limitations, and contribution evidence.

**Exit:** a fresh clone can reproduce the demo without secrets or prebuilt databases.

## MVP boundaries

Included:

- Local synthetic challenges
- One short text answer per challenge
- Learner and administrator roles
- Personal progress
- Topic discussion rooms
- Localhost-first deployment

Deferred:

- Running untrusted code
- Automatically deployed vulnerable machines
- File uploads from learners
- Team competitions and dynamic scoring
- Public internet hosting
- Redis, message brokers, Docker, and cloud deployment
