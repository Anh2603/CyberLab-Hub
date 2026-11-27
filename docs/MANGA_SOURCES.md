# Manga catalog metadata

The committed `internal/catalog/manga_seed.json` is a reproducible local metadata
snapshot taken on 2026-11-27. The application needs no external API at runtime.
Each record includes its AniList media page, the chapter-count source, and the
snapshot date. Required application fields are imported into catalog SQLite.

- Title identity, creators (Story/Art roles), genres, publication status, and
  completed-series chapter totals: [AniList GraphQL API](https://graphql.anilist.co),
  with a per-record `source_url` such as [Naruto](https://anilist.co/manga/30011).
- One Piece's ongoing chapter baseline (1199):
  [VIZ official chapter list](https://www.viz.com/shonenjump/chapters/one-piece).
  Its creator was cross-checked against that publisher page.
- Representative publisher cross-check:
  [Attack on Titan / Kodansha](https://kodansha.us/series/attack-on-titan/).

Descriptions are short original summaries written for this application. No
synopsis blocks, manga pages, scans, cover images, or chapter text are copied.
Genres follow the metadata source. Chapter totals may include bonus chapters,
so they need not equal the last numbered main-story chapter.

## Local demonstration versus actual publication

An admin release in MangaHub changes the local classroom catalog and triggers
the assessed gRPC/UDP flow. It is not a claim that the real publisher released a
chapter. Restarts and explicit catalog refreshes preserve local release counts.
For example, the automated tests increase Naruto from 700 to 701 in a temporary
database solely to verify that flow.

## Existing fictional dataset

`cmd/catalog-refresh` inserts real records only if their IDs are absent, then
hides old fictional entries whose original metadata fingerprints still match.
It never deletes them or changes personal library IDs. This keeps historical
sample progress separate from real manga; progress is not reassigned to an
unrelated work. Startup itself continues to seed only an empty catalog.
