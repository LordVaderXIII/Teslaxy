
import json
import time
import re
from datetime import datetime
from playwright.sync_api import sync_playwright, expect

def test_filter_indicator():
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        page = browser.new_page()

        # Generate dynamic date for "Today"
        now = datetime.now()
        date_key = now.strftime("%Y-%m-%d") # e.g. 2023-10-27
        # ISO timestamp
        timestamp = now.isoformat()

        # Mock APIs
        def handle_clips(route):
            print("Handling /api/clips")
            route.fulfill(
                status=200,
                content_type="application/json",
                body=json.dumps([
                    {
                        "ID": 123,
                        "timestamp": timestamp,
                        "event": "Recent",
                        "city": "Test City",
                        "video_files": [],
                        "date_key": date_key # Sidebar uses this if present
                    }
                ])
            )

        page.route("**/api/clips", handle_clips)
        page.route("**/api/version", lambda route: route.fulfill(
            status=200,
            body=json.dumps({"latestVersion": "v1.0.0", "releases": []})
        ))

        # Navigate
        print("Navigating...")
        page.goto("http://localhost:5173")

        # Wait for app to load
        print("Waiting for Test City...")
        try:
            expect(page.get_by_text("Test City")).to_be_visible(timeout=10000)
        except Exception as e:
            print(f"Failed to find Test City: {e}")
            page.screenshot(path="verification_filters_failed_load.png")
            raise e

        # Locate Filter Button
        # Initial state: aria-label="Show filters"
        filter_button = page.get_by_label("Show filters", exact=True)
        expect(filter_button).to_be_visible()

        # Check Initial State (Should NOT have active class)
        print("Checking initial state...")

        # Initial check: Should be gray, NOT blue
        expect(filter_button).to_have_class(re.compile(r"bg-gray-900"))
        expect(filter_button).not_to_have_class(re.compile(r"bg-blue-900/20"))

        # Open Filters
        print("Opening filters...")
        filter_button.click()

        # Find "Recent Clips" checkbox and uncheck it
        recent_checkbox = page.get_by_label("Recent Clips")
        expect(recent_checkbox).to_be_checked()
        recent_checkbox.uncheck()

        # Close filters
        # Button aria-label changes to "Hide filters" when open.
        close_button = page.get_by_label("Hide filters")
        close_button.click()

        # Check Active State
        print("Checking active state...")

        # Expect "Show filters (Active)"
        active_button = page.get_by_label("Show filters (Active)")
        expect(active_button).to_be_visible(timeout=2000)

        # Check active class
        expect(active_button).to_have_class(re.compile(r"bg-blue-900/20"))

        print("Verification successful!")

        # Screenshot for verification
        page.screenshot(path="frontend/verification_filters.png")
        print("Screenshot saved to frontend/verification_filters.png")

        browser.close()

if __name__ == "__main__":
    test_filter_indicator()
