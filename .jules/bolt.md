## 2025-10-27 - Lazy Loading Large Modals
**Learning:** Heavy libraries like `leaflet` and `react-leaflet` can significantly impact initial bundle size if statically imported, even if the component (e.g., `MapModal`) is only conditionally rendered.
**Action:** Use `React.lazy` and `Suspense` for large, infrequent components like map modals or dashboards to split them into separate chunks that only load on demand.

## 2025-10-28 - JSON Payload Optimization
**Learning:** Redundant foreign keys (e.g., `clip_id` inside a nested `VideoFile` object) can bloat JSON payloads significantly. However, removing Primary Keys (`ID`) is unsafe as frontend apps may rely on them for list keys.
**Action:** Audit Go struct JSON tags to hide (`json:"-"`) redundant foreign keys in nested relationships while preserving Primary Keys (`json:"ID"`).

## 2026-02-04 - Batch Ingestion Optimization
**Learning:** When batch processing files for database ingestion, bulk-fetching existing records into a map (O(1) lookup) is significantly faster than querying for each item (O(N) queries). However, failing to update the map during processing can cause duplicates if the batch contains repeated items.
**Action:** Always consider bulk-fetch + in-memory lookup patterns for loops that perform database existence checks, and update the lookup map on insertion to handle batch duplicates.
