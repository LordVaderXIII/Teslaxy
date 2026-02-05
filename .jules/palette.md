## 2024-05-24 - Actionable Empty States
**Learning:** Users are more likely to recover from "no results" screens when provided with specific actions (e.g., "Reset filters") rather than just a status message.
**Action:** When implementing empty states for filtered lists, always include a conditional button to clear the active filter or navigate to a default state.

## 2026-01-19 - Contextual Date Navigation
**Learning:** In date-heavy interfaces (calendars, timelines), users often get "lost" in past/future states. Providing a contextual "Back to Today" action reduces cognitive load and click fatigue.
**Action:** When implementing date pickers or timelines, always include a conditional "Today" shortcut if the view deviates from the current date.

## 2026-02-05 - Visibility of Active Filters
**Learning:** Users often forget they have filters applied if the filter control looks identical in both states, leading to confusion about missing data.
**Action:** When a filter is active, change the filter button's visual state (color, border) and update the accessible name to explicitly indicate "Active".
