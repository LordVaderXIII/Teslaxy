import time
from datetime import datetime, timezone
from playwright.sync_api import sync_playwright, expect
import json

def run(playwright):
    browser = playwright.chromium.launch(headless=True)
    page = browser.new_page()

    # Mock API
    now = datetime.now(timezone.utc).isoformat()

    page.route("**/api/version", lambda route: route.fulfill(
        status=200,
        content_type="application/json",
        body='{"version": "0.1.17", "commit": "abc1234"}'
    ))

    page.route("**/api/transcode/status", lambda route: route.fulfill(
        status=200,
        content_type="application/json",
        body='{"encoder": "cpu", "hw_accel": false}'
    ))

    # Mock clips (Sidebar needs clips to show something)
    clips = [
        {
            "ID": 1,
            "timestamp": now,
            "event": "Sentry",
            "city": "Test City",
            "reason": "sentry_aware_object_detection",
            "video_files": []
        }
    ]

    page.route("**/api/clips", lambda route: route.fulfill(
        status=200,
        content_type="application/json",
        body=json.dumps(clips)
    ))

    page.goto("http://localhost:4173/")

    try:
        # Wait for Sidebar to load
        page.wait_for_selector("text=Library")

        # Check default state - Use text because aria-label masks it
        # Or use the exact aria label
        filter_btn = page.get_by_role("button", name="Show filters")
        expect(filter_btn).to_be_visible()

        # Click filter button to open dropdown
        filter_btn.click()

        # Uncheck "Recent Clips"
        page.get_by_label("Recent Clips").uncheck()

        # Now filters are active.
        # Close dropdown (click outside)
        page.mouse.click(0, 0)

        # Take screenshot of active state
        page.screenshot(path="verification_active.png")
    except Exception as e:
        print(f"Error: {e}")
        page.screenshot(path="verification_failed.png")
        raise e

    browser.close()

with sync_playwright() as playwright:
    run(playwright)
