## 2025-10-27 - Lazy Loading Large Modals
**Learning:** Heavy libraries like `leaflet` and `react-leaflet` can significantly impact initial bundle size if statically imported, even if the component (e.g., `MapModal`) is only conditionally rendered.
**Action:** Use `React.lazy` and `Suspense` for large, infrequent components like map modals or dashboards to split them into separate chunks that only load on demand.

## 2026-01-14 - Composite Index for Preloads
**Learning:** GORM `Preload` queries using `IN` clauses can avoid expensive filesorts if the `ORDER BY` clause matches a composite index starting with the foreign key (e.g., `ORDER BY clip_id, timestamp` matches `(clip_id, timestamp)`).
**Action:** When preloading associations that require sorting, ensure a composite index exists on `(foreign_key, sort_key)` and explicitly include the foreign key in the `Order` clause.
