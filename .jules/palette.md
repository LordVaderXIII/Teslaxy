## 2024-05-24 - Actionable Empty States
**Learning:** Users are more likely to recover from "no results" screens when provided with specific actions (e.g., "Reset filters") rather than just a status message.
**Action:** When implementing empty states for filtered lists, always include a conditional button to clear the active filter or navigate to a default state.

## 2026-01-16 - Graceful Image Fallbacks
**Learning:** Broken image icons disrupt visual flow and decrease perceived quality. Users expect a seamless fallback even when data is missing.
**Action:** Encapsulate image rendering in a reusable component (like `ThumbnailImage`) that handles `onError` state internally, falling back to a semantic placeholder.
