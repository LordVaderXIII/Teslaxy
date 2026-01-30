## 2024-05-24 - Actionable Empty States
**Learning:** Users are more likely to recover from "no results" screens when provided with specific actions (e.g., "Reset filters") rather than just a status message.
**Action:** When implementing empty states for filtered lists, always include a conditional button to clear the active filter or navigate to a default state.

## 2026-01-19 - Contextual Date Navigation
**Learning:** In date-heavy interfaces (calendars, timelines), users often get "lost" in past/future states. Providing a contextual "Back to Today" action reduces cognitive load and click fatigue.
**Action:** When implementing date pickers or timelines, always include a conditional "Today" shortcut if the view deviates from the current date.

## 2026-01-30 - Active Filter Indicators
**Learning:** Hidden filters are a common source of user confusion ("Where did my files go?"). A subtle text count is insufficient.
**Action:** Apply a distinct visual state (e.g., brand color tint) and updated ARIA labels to filter triggers whenever they are actively restricting content.
