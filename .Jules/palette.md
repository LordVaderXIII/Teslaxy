## 2025-05-23 - Interactive Lists Accessibility
**Learning:** List items with `onClick` handlers must be keyboard accessible. Using `<div>` requires `role="button"`, `tabIndex="0"`, and `onKeyDown` handlers for Enter/Space keys, but semantic `<button>` elements handle this natively and are preferred.
**Action:** Replace interactive `<div>` elements with `<button>` elements where possible, or ensure full ARIA compliance if layout constraints require `<div>`.

## 2025-05-24 - Icon Button Accessibility
**Learning:** Key interaction points like the Play/Pause button were using inline SVGs without labels, making them invisible to screen readers. The app already has `lucide-react`, so using it ensures consistency and simplifies code.
**Action:** Audit other icon-only buttons (Calendar, Sidebar toggles) and replace inline SVGs with labeled Lucide icons.

## 2025-05-25 - Media Controls Accessibility
**Learning:** Media players often rely heavily on iconography (Play, Pause, Skip) without text labels, creating a major barrier for screen reader users. Adding aria-labels and tooltips is a critical, low-effort fix.
**Action:** Standardize usage of aria-label and title attributes on all player controls (Play, Pause, Skip, 3D toggle) to ensure both screen reader and mouse hover accessibility.

## 2025-05-26 - Modal Dismissal Interactions
**Learning:** Users instinctively press `Escape` or click the backdrop to close modals. Relying solely on a close button creates friction and can feel "broken" or unresponsive.
**Action:** Always add `useEffect` listeners for the `Escape` key and `onClick` handlers on the modal backdrop to trigger the close action.

## 2025-05-27 - Context-Preserving Refresh
**Learning:** When refreshing list data, users expect their current selection and context (e.g., scroll position, active item) to remain stable unless the item was deleted.
**Action:** Ensure refresh actions update the dataset in place without triggering a full reset of the selection state or "jumping" the user to the start of the list.

## 2025-05-28 - Calendar Navigation Accessibility
**Learning:** Calendar components often use simple numbers for day buttons, which provides poor context for screen reader users (hearing "1, 2, 3" instead of "May 1st"). Navigational chevrons also frequently lack labels.
**Action:** Ensure calendar day buttons have `aria-label` attributes containing the full date and status (e.g., "May 1st, 2025, has footage"), and navigational arrows are clearly labeled "Previous Month" and "Next Month".

## 2025-12-21 - Consistent Focus States
**Learning:** While some components (Sidebar, Player) had excellent focus states, others (Calendar, MapModal) relied on browser defaults or had none, creating a disjointed keyboard navigation experience.
**Action:** Audit all interactive elements and enforce a consistent `focus-visible` ring style (e.g., `ring-2 ring-blue-500`) to ensure users always know where they are, regardless of the component.

## 2025-10-27 - Graceful Image Degradation
**Learning:** Relying on the browser's default broken image icon for missing thumbnails creates a jarring, unpolished experience, especially in a media-centric app. Using a semantic, colored fallback matching the empty state maintains visual consistency and trust.
**Action:** Implement a reusable `ThumbnailImage` component with `onError` handling to automatically swap failed images for a branded placeholder.

## 2025-05-29 - Interactive Media Grids
**Learning:** Grids of media items (like camera views) often use `div`s with `onClick` but lack keyboard accessibility. Using `<button>` makes them natively focusable and actionable.
**Action:** Use `<button>` for any selectable media tile.
