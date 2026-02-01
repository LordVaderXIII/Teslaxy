## 2024-05-24 - Actionable Empty States
**Learning:** Users are more likely to recover from "no results" screens when provided with specific actions (e.g., "Reset filters") rather than just a status message.
**Action:** When implementing empty states for filtered lists, always include a conditional button to clear the active filter or navigate to a default state.

## 2026-01-19 - Contextual Date Navigation
**Learning:** In date-heavy interfaces (calendars, timelines), users often get "lost" in past/future states. Providing a contextual "Back to Today" action reduces cognitive load and click fatigue.
**Action:** When implementing date pickers or timelines, always include a conditional "Today" shortcut if the view deviates from the current date.

## 2026-05-21 - Visual State for Active Filters
**Learning:** Collapsible filter panels can hide system state, causing users to forget that data is being filtered. A simple text label (e.g., "3 shown") is often insufficient as a primary signal.
**Action:** When a collapsible filter affects the view, the trigger button itself must visually indicate the "Active" state (via color/style) and convey this state to screen readers via ARIA labels.
