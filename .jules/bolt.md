## 2025-10-27 - Lazy Loading Large Modals
**Learning:** Heavy libraries like `leaflet` and `react-leaflet` can significantly impact initial bundle size if statically imported, even if the component (e.g., `MapModal`) is only conditionally rendered.
**Action:** Use `React.lazy` and `Suspense` for large, infrequent components like map modals or dashboards to split them into separate chunks that only load on demand.

## 2025-10-28 - JSON Payload Optimization
**Learning:** Redundant foreign keys (e.g., `clip_id` inside a nested `VideoFile` object) can bloat JSON payloads significantly. However, removing Primary Keys (`ID`) is unsafe as frontend apps may rely on them for list keys.
**Action:** Audit Go struct JSON tags to hide (`json:"-"`) redundant foreign keys in nested relationships while preserving Primary Keys (`json:"ID"`).

## 2025-10-29 - Scanner N+1 Query Optimization
**Learning:** Checking for file existence one-by-one inside a loop using `s.DB.Where(...).First(...)` creates a classic N+1 query performance bottleneck, especially when scanning directories with many files.
**Action:** Bulk-fetch existing identifiers (e.g., `file_path`) into a map *before* the loop, reducing database interaction to a single `SELECT`.
