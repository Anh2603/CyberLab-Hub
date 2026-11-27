# MangaHub verification

The conversion reuses authenticated networking but introduces actual MangaHub
metadata, library, progress, and chapter release workflows.

## Repeatable integration evidence

Run `go test -count=1 -run TestManga -timeout 60s ./internal/tui`.

| Case | Expected | Observed in conversion run |
| --- | --- | --- |
| Catalog | 30 complete records, at least four genres | 30 real titles, 14 genres |
| Empty/unknown search | Empty list / missing-ID error | Both verified |
| Personal library | Account ownership; no duplicate add | Duplicate rejected; another account could not update owner's entry |
| Chapter validation | Reject negative, above-total, unsupported status | All rejected |
| TCP | Other same-account session receives exact committed version/chapter | Received `progress_updated`, chapter 5, version 2; origin and different user received none |
| Visible TUI progress | Second TUI updates without manual refresh | Rendered library changed to chapter 7/version 2 after TCP event |
| UDP release | Two subscribed clients receive new total; non-subscriber receives none | Local Naruto test release to chapter 701 delivered to both subscribers, none to non-subscriber |
| Release authorization | Non-admin and non-increasing total rejected | Both rejected |
| TUI notification | Incoming datagram visible in widget | `New chapter: Naruto` displayed |
| TUI controls | Signup → add title → edit reading progress | Form actions saved reading/chapter 8 |
| Chat | Sender and same room receive; other room receives none | Both general-room clients received; mystery-room client received none |
| SQLite restart | Progress/version and released chapter remain | Reopened test databases retained chapter 701 and library version 3; reseeding did not overwrite |
| Catalog stop | Bounded error rather than hanging | HTTP client failed within two-second assertion |

These are automated observations on temporary databases and actual loopback
listeners. They do not claim that the pair has performed the live oral defense.

The actual `cmd/catalog` and `cmd/backend` programs were also run as separate
processes on private test ports. A headless Chrome smoke test passed signup,
30-title browsing, library add, persisted chapter edit, admin release, timestamped
chat, and mobile horizontal-overflow checks. Desktop and mobile screenshots were
visually inspected. The browser is supplementary; TCP/UDP evidence is from TUI.

`go test ./...` and `go vet ./...` passed during conversion. The Windows
`go test -race` attempt was blocked at compilation because `gcc` was not on PATH;
therefore no successful race-detector result is claimed. Run the documented race
command on a supported Go/C toolchain before using it as defense evidence.

## Additional regression coverage

Existing auth, TCP, UDP, WebSocket, storage, and configuration suites remain.
They cover malformed/oversized messages, token checks, unavailable receivers,
unsubscribe/expiry, room isolation, cleanup, and bounded socket operations.

The old CyberLab-specific tests only preserve migration infrastructure; MangaHub
feature evidence comes from the `TestManga*` tests above.

## Limitations to explain

UDP has no delivery guarantee. Chat history is not persisted. TCP connections
require reopening/signing in after a transport failure. Redis is not implemented.
The web interface supplements rather than replaces the required Go tview demo.
