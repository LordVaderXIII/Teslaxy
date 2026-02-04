## 2024-05-24 - Actionable Empty States
**Learning:** Users are more likely to recover from "no results" screens when provided with specific actions (e.g., "Reset filters") rather than just a status message.
**Action:** When implementing empty states for filtered lists, always include a conditional button to clear the active filter or navigate to a default state.

## 2026-01-19 - Contextual Date Navigation
**Learning:** In date-heavy interfaces (calendars, timelines), users often get "lost" in past/future states. Providing a contextual "Back to Today" action reduces cognitive load and click fatigue.
**Action:** When implementing date pickers or timelines, always include a conditional "Today" shortcut if the view deviates from the current date.

## 2025-02-17 - Visible Active State for Filters
**Learning:** Users may forget filters are active if the UI doesn't explicitly show it. A subtle text change isn't enough.
**Action:** When applying filters, change the trigger button's visual state (e.g., background color) and ARIA label to explicitly indicate "Active" mode.
