## 2024-05-24 - Actionable Empty States
**Learning:** Users are more likely to recover from "no results" screens when provided with specific actions (e.g., "Reset filters") rather than just a status message.
**Action:** When implementing empty states for filtered lists, always include a conditional button to clear the active filter or navigate to a default state.

## 2026-01-19 - Contextual Date Navigation
**Learning:** In date-heavy interfaces (calendars, timelines), users often get "lost" in past/future states. Providing a contextual "Back to Today" action reduces cognitive load and click fatigue.
**Action:** When implementing date pickers or timelines, always include a conditional "Today" shortcut if the view deviates from the current date.

## 2026-01-24 - Empty State Visual Anchors
**Learning:** In dark mode interfaces, text-only empty states can disappear into the background or look like data glitches. Adding a low-contrast icon anchors the empty state as a deliberate UI element.
**Action:** When implementing empty states for specific content blocks (like camera views), always pair the descriptive text with a relevant icon (stroke-width 1.5, size 24-32px).
