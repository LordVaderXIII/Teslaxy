## 2025-10-27 - Lazy Loading Large Modals
**Learning:** Heavy libraries like `leaflet` and `react-leaflet` can significantly impact initial bundle size if statically imported, even if the component (e.g., `MapModal`) is only conditionally rendered.
**Action:** Use `React.lazy` and `Suspense` for large, infrequent components like map modals or dashboards to split them into separate chunks that only load on demand.

## 2025-10-28 - JSON Payload Optimization
**Learning:** Redundant foreign keys (e.g., `clip_id` inside a nested `VideoFile` object) can bloat JSON payloads significantly. However, removing Primary Keys (`ID`) is unsafe as frontend apps may rely on them for list keys.
**Action:** Audit Go struct JSON tags to hide (`json:"-"`) redundant foreign keys in nested relationships while preserving Primary Keys (`json:"ID"`).

## 2025-10-27 - Date Parsing in Render Loops
**Learning:** Parsing date strings (`new Date()`) inside `useMemo` or render loops for large lists (like `Sidebar` clips) is expensive (O(N) per render/filter).
**Action:** Enrich data objects with pre-parsed `Date` objects and formatted keys (e.g., `date_key`) during the initial data fetch/transform phase (`mergeClips`) to make downstream filtering and rendering O(1).
