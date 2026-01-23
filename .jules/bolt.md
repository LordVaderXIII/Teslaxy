## 2025-10-27 - Lazy Loading Large Modals
**Learning:** Heavy libraries like `leaflet` and `react-leaflet` can significantly impact initial bundle size if statically imported, even if the component (e.g., `MapModal`) is only conditionally rendered.
**Action:** Use `React.lazy` and `Suspense` for large, infrequent components like map modals or dashboards to split them into separate chunks that only load on demand.

## 2025-10-28 - JSON Payload Optimization
**Learning:** Redundant foreign keys (e.g., `clip_id` inside a nested `VideoFile` object) can bloat JSON payloads significantly. However, removing Primary Keys (`ID`) is unsafe as frontend apps may rely on them for list keys.
**Action:** Audit Go struct JSON tags to hide (`json:"-"`) redundant foreign keys in nested relationships while preserving Primary Keys (`json:"ID"`).

## 2025-10-29 - O(N²) Array Concatenation
**Learning:** Using `Array.concat` inside a loop to merge arrays (e.g., `allFiles = allFiles.concat(files)`) creates a new array allocation for every iteration, resulting in O(N²) complexity and significant performance degradation for large datasets.
**Action:** Use `Array.push(...items)` or a manual loop to append items to a single mutable array, ensuring O(N) complexity and minimal memory overhead.
