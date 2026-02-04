## 2025-10-27 - Lazy Loading Large Modals
**Learning:** Heavy libraries like `leaflet` and `react-leaflet` can significantly impact initial bundle size if statically imported, even if the component (e.g., `MapModal`) is only conditionally rendered.
**Action:** Use `React.lazy` and `Suspense` for large, infrequent components like map modals or dashboards to split them into separate chunks that only load on demand.

## 2025-10-28 - JSON Payload Optimization
**Learning:** Redundant foreign keys (e.g., `clip_id` inside a nested `VideoFile` object) can bloat JSON payloads significantly. However, removing Primary Keys (`ID`) is unsafe as frontend apps may rely on them for list keys.
**Action:** Audit Go struct JSON tags to hide (`json:"-"`) redundant foreign keys in nested relationships while preserving Primary Keys (`json:"ID"`).

## 2025-10-28 - N+1 Query in File Scanner
**Learning:** Iterating over a file list and querying the DB for existence inside the loop (N+1) is a major bottleneck during scanning, even with a local SQLite DB. Memory can be misleading about optimization status.
**Action:** Always verify code implementation against memory/docs. Use bulk fetching (`WHERE ... IN (...)`) and hash maps for O(1) existence checks in batch processing functions.
