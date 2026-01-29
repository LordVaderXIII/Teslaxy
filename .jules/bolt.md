## 2025-10-27 - Lazy Loading Large Modals
**Learning:** Heavy libraries like `leaflet` and `react-leaflet` can significantly impact initial bundle size if statically imported, even if the component (e.g., `MapModal`) is only conditionally rendered.
**Action:** Use `React.lazy` and `Suspense` for large, infrequent components like map modals or dashboards to split them into separate chunks that only load on demand.

## 2025-10-28 - JSON Payload Optimization
**Learning:** Redundant foreign keys (e.g., `clip_id` inside a nested `VideoFile` object) can bloat JSON payloads significantly. However, removing Primary Keys (`ID`) is unsafe as frontend apps may rely on them for list keys.
**Action:** Audit Go struct JSON tags to hide (`json:"-"`) redundant foreign keys in nested relationships while preserving Primary Keys (`json:"ID"`).

## 2025-10-29 - N+1 Query in File Scanner
**Learning:** The `addFilesToClip` function was performing a `SELECT` query for every single video file in a loop to check for existence, causing significant database overhead during scanning. Memory incorrectly stated this was optimized.
**Action:** Always verify "known" optimizations in the codebase. Replace iterative existence checks with a bulk-fetch strategy (loading all existing records for the parent entity into a map) to reduce DB round-trips from O(N) to O(1).
