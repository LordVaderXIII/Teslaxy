## 2025-10-27 - Lazy Loading Large Modals
**Learning:** Heavy libraries like `leaflet` and `react-leaflet` can significantly impact initial bundle size if statically imported, even if the component (e.g., `MapModal`) is only conditionally rendered.
**Action:** Use `React.lazy` and `Suspense` for large, infrequent components like map modals or dashboards to split them into separate chunks that only load on demand.

## 2025-10-27 - Stabilizing Props for Memoized Components
**Learning:** Passing high-frequency changing props (like `currentTime` in a video player) to memoized components that don't use them (e.g., secondary camera views) breaks `React.memo` optimization, causing unnecessary re-renders (e.g., 60Hz per camera).
**Action:** Explicitly pass static values (e.g., `0` or `undefined`) for unused props to memoized components to preserve referential equality and prevent re-renders.
