## 2025-10-27 - Lazy Loading Large Modals
**Learning:** Heavy libraries like `leaflet` and `react-leaflet` can significantly impact initial bundle size if statically imported, even if the component (e.g., `MapModal`) is only conditionally rendered.
**Action:** Use `React.lazy` and `Suspense` for large, infrequent components like map modals or dashboards to split them into separate chunks that only load on demand.

## 2025-10-28 - JSON Payload Optimization
**Learning:** Redundant foreign keys (e.g., `clip_id` inside a nested `VideoFile` object) can bloat JSON payloads significantly. However, removing Primary Keys (`ID`) is unsafe as frontend apps may rely on them for list keys.
**Action:** Audit Go struct JSON tags to hide (`json:"-"`) redundant foreign keys in nested relationships while preserving Primary Keys (`json:"ID"`).

## 2025-10-29 - List View Over-fetching
**Learning:** Preloading heavy relationships (like `Telemetry` with its JSON blobs) for list views that don't display them causes unnecessary DB load and bandwidth usage.
**Action:** Use `Select` clauses in GORM to explicitly fetch only the fields needed for the specific view (e.g., `id`, `timestamp`, `reason`) and avoid `Preload` for unused relationships in list endpoints.
