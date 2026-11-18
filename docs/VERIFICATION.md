# Verification Record

This record contains observed results from the implementation environment. Re-run the commands on the final demonstration machine and update any result that changes.

## Automated checks

| Check | Expected | Observed |
| --- | --- | --- |
| `go test -count=1 ./...` | All packages pass | Passed for auth, backend, catalog, config, store, TCP, UDP, WebSocket, and TUI client packages on 2026-11-18. |
| `go vet ./...` | No diagnostics | Passed on 2026-11-18 after the TUI send fix. |
| Build three programs | Backend, catalog, and TUI compile | `go build ./cmd/backend ./cmd/catalog ./cmd/tui` passed. |
| Protocol Buffer lint | Contract follows configured standard | `buf lint` and `buf format --diff --exit-code` passed. |
| Race detector | Run where supported | Not runnable on this Windows host because Go reported that `gcc` was not installed. This is an environment limitation, not recorded as a passing check. |
| Statement coverage | Measure gaps rather than assume completeness | 39.8% total on 2026-11-18, including command entry points and generated code. Auth: 86.4%; TCP: 73.9%; UDP: 75.3%; WebSocket: 76.5%. The interactive widget layer remains manually unverified. |
| Vulnerability scan | No reachable known vulnerabilities | `govulncheck` v1.10.0 passed on 2026-11-18 with Go 1.27.4 and quic-go v0.59.1: 0 reachable findings and 0 imported-package findings. It still reported 3 module-level advisories whose affected code is not called. |

The coverage figure is the Phase 9 snapshot before the final keyboard-navigation adjustment. It does not represent visual verification of the TUI. Test, vet, and build were rerun successfully after that adjustment using Go 1.27.4.

The first vulnerability scan on Go 1.27.1 with quic-go v0.59.0 reported 14 reachable findings. The project now selects Go 1.27.4 through its toolchain directive and pins the patched quic-go release; the rerun returned zero reachable findings. Do not interpret this scan as proof that the application is free of every security defect.

## Feature evidence

| Feature/protocol | Test or condition | Expected | Observed |
| --- | --- | --- | --- |
| Accounts/HTTP | Register, duplicate case-insensitive username, wrong password, valid JWT | Correct status and isolation | Automated handler tests pass; passwords are bcrypt hashes. |
| Persistence/SQLite | Close and reopen backend database | User remains | Store persistence test passes. |
| Catalog/gRPC | Publish, search, verify correct/wrong answer, archive | Typed RPC results and statuses | In-memory real gRPC client/server workflow passes. |
| Catalog failure | Stop gRPC server during HTTP search | Controlled response within deadline | Integration test receives 503 or 504 in less than one second. |
| Progress/TCP | Two sessions for user A, one for user B | A's other session receives; origin and B do not | Real TCP receiver test passes; disconnect registry cleanup is asserted. |
| Publication/UDP | Subscribe, notify, unsubscribe, expire, unreachable receiver | Best-effort delivery without server crash | Real UDP socket tests pass. |
| Discussion/WebSocket | Two clients in one room, third in another | Same-room delivery and room isolation | Authenticated WebSocket integration test passes; disconnect count is asserted. |
| TUI networking | HTTP, TCP, UDP and WebSocket send/receive/cancel | UI-facing clients receive real events | TUI client integration tests pass, including WebSocket event-channel closure after cancellation. |
| Process smoke test | Run catalog and backend as separate hidden processes | Health, seed search, solve, and progress work | Observed 3 seeded challenges, correct solve, and one persisted progress item. |

## Security checks represented in tests or code

- JWT expiry, issuer, algorithm, role, and subject validation.
- Case-insensitive unique usernames and generic wrong-credential response.
- HTTP body, TCP line, UDP datagram, WebSocket message, and gRPC message limits.
- Server-derived identity for protected state and discussion messages.
- Answer hashes remain only in catalog storage and never appear in public protobuf messages.
- Loopback-only default listeners, bounded timeouts, graceful shutdown, and no secret logging.

## Remaining manual checks

- Exercise keyboard focus and layout at the exact terminal size used for the demo.
- Repeat the full demo after a clean database start and after a backend restart.
- Run the race detector on a supported environment with a C compiler.
- Confirm the instructor has approved CyberLab Hub as a domain substitution for MangaHub.
