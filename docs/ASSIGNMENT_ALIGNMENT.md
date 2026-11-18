# Assignment Alignment and Approval Gap

## Important scope decision

The supplied Version 3.0 specification names MangaHub as the authoritative project and defines manga-specific acceptance criteria. CyberLab Hub preserves the required architecture, technology choices, independent processes, persistence, TUI, and five integrated networking paths, but it changes the product domain.

Obtain written instructor approval before treating CyberLab Hub as a compliant replacement. Without that approval, technical similarity does not guarantee acceptance because the current system has challenges and solved progress rather than manga, personal libraries, chapter numbers, and chapter releases.

## Technical mapping

| Required technical responsibility | CyberLab Hub implementation | Status |
| --- | --- | --- |
| Go Gin backend | Authenticated HTTP challenge workflow | Implemented |
| Go tview TUI | Login, browser, solve, progress, notifications, discussion, admin publication | Implemented |
| SQLite through `database/sql` | Separate backend and catalog databases | Implemented |
| HTTP | Accounts, challenge discovery, submissions, progress, admin actions | Implemented |
| TCP | Private solved-progress event to other sessions of the same user | Implemented |
| UDP | Best-effort newly published challenge notification | Implemented |
| WebSocket | Authenticated topic discussion rooms | Implemented |
| gRPC + Protobuf | Separate challenge catalog service with generated stubs | Implemented |
| Service failure handling | Bounded gRPC deadline mapped to HTTP 503/504 | Implemented |

## Semantic differences requiring approval

| MangaHub criterion | CyberLab equivalent | Difference |
| --- | --- | --- |
| Search manga and show author, genre, chapter metadata | Search challenges and show category, difficulty, description | Domain fields differ. |
| Personal library and reading status | Solved challenge progress | No plan-to-read/reading/completed state. |
| Integer chapter update bounded by total chapters | Server-verified challenge answer | Different validation rule. |
| New chapter release updates chapter count | Challenge publication creates or updates catalog content | Different admin action. |
| `SearchManga`, `GetManga`, `UpdateChapters` RPCs | `SearchChallenges`, `GetChallenge`, `VerifyAnswer`, `PublishChallenge`, `ArchiveChallenge` | Typed boundary is present, but RPC names and semantics differ. |

## If approval is denied

Do not merely rename screens. Rework the catalog schema/proto, backend persistence, HTTP routes, TCP event, UDP event, TUI fields, seeds, tests, and report so that the observable workflows exactly implement F01-F06 from the MangaHub specification.
