## 2025-05-23 - Interactive Lists Accessibility
**Learning:** List items with `onClick` handlers must be keyboard accessible. Using `<div>` requires `role="button"`, `tabIndex="0"`, and `onKeyDown` handlers for Enter/Space keys, but semantic `<button>` elements handle this natively and are preferred.
**Action:** Replace interactive `<div>` elements with `<button>` elements where possible, or ensure full ARIA compliance if layout constraints require `<div>`.
