## 2025-05-23 - Interactive Lists Accessibility
**Learning:** List items with `onClick` handlers must be keyboard accessible. Using `<div>` requires `role="button"`, `tabIndex="0"`, and `onKeyDown` handlers for Enter/Space keys, but semantic `<button>` elements handle this natively and are preferred.
**Action:** Replace interactive `<div>` elements with `<button>` elements where possible, or ensure full ARIA compliance if layout constraints require `<div>`.

## 2025-05-24 - Icon Button Accessibility
**Learning:** Key interaction points like the Play/Pause button were using inline SVGs without labels, making them invisible to screen readers. The app already has `lucide-react`, so using it ensures consistency and simplifies code.
**Action:** Audit other icon-only buttons (Calendar, Sidebar toggles) and replace inline SVGs with labeled Lucide icons.
