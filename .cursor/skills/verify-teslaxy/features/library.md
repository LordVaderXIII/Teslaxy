# Library browse

The library lists scanner-owned logical events (Sentry / Saved / Recent) for the sidebar.

## Sub-features

- `library-list` returns events + facets from `GET /api/library`.
- `library-files` returns per-camera files for one event via `GET /api/library/files?id=`.
- `library-protect-flag` surfaces `protected` on each event row.

## How to get to it (user POV)

- Open the Teslaxy UI; the sidebar loads `/api/library`.
- Select an event to load `/api/library/files`.

## Driving it with curl

- `curl -sf 'http://127.0.0.1:18080/api/library'`
- `curl -sf 'http://127.0.0.1:18080/api/library/files?id=1'`
- Proof: JSON has `events` and `facets`; soft-deleted clips are omitted.

## Gotchas

- Grouping is owned by the scanner (`SourceDir` + `event.json`), not the frontend.
- Empty footage yields empty `events` — that is success, not a hang.
