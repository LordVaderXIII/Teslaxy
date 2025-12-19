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
