
import os
import time
from playwright.sync_api import sync_playwright, expect

def verify_shortcuts(page):
    # Mock API responses to avoid backend dependency
    page.route("**/api/clips", lambda route: route.fulfill(
        status=200,
        content_type="application/json",
        body='[{"ID": 1, "timestamp": "2023-01-01T12:00:00Z", "event": "Sentry", "city": "Test City"}]'
    ))
    page.route("**/api/clips/1", lambda route: route.fulfill(
        status=200,
        content_type="application/json",
        body='{"ID": 1, "timestamp": "2023-01-01T12:00:00Z", "event": "Sentry", "city": "Test City", "video_files": []}'
    ))
    page.route("**/api/version", lambda route: route.fulfill(
        status=200,
        content_type="application/json",
        body='{"latestVersion": "v1.0.0", "releases": []}'
    ))
    page.route("**/api/transcode/status", lambda route: route.fulfill(
        status=200,
        content_type="application/json",
        body='{"encoder": "test", "hw_accel": false}'
    ))

    # Go to app
    page.goto("http://localhost:5173")

    # Wait for app to load
    page.wait_for_selector("h1:has-text('Teslaxy')")

    # 1. Verify '?' key opens shortcuts
    page.keyboard.press("?")

    # Expect modal to appear
    expect(page.get_by_role("dialog")).to_be_visible()
    expect(page.get_by_text("Keyboard Shortcuts")).to_be_visible()

    # Take screenshot of open modal
    page.screenshot(path="frontend/verification/shortcuts_modal.png")

    # Close modal
    page.keyboard.press("Escape")
    expect(page.get_by_role("dialog")).not_to_be_visible()

    # 2. Verify button in sidebar
    # Wait for sidebar to be visible (it should be)
    # The button has aria-label="Keyboard Shortcuts"
    # Note: On mobile width it might be hidden or different, but we run desktop size by default

    # Ensure viewport is desktop size
    page.set_viewport_size({"width": 1280, "height": 720})

    help_button = page.get_by_role("button", name="Keyboard Shortcuts")
    expect(help_button).to_be_visible()

    help_button.click()
    expect(page.get_by_role("dialog")).to_be_visible()

    print("Verification successful!")

if __name__ == "__main__":
    with sync_playwright() as p:
        browser = p.chromium.launch()
        page = browser.new_page()
        try:
            verify_shortcuts(page)
        except Exception as e:
            print(f"Error: {e}")
            page.screenshot(path="frontend/verification/error.png")
        finally:
            browser.close()
