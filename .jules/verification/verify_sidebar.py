import json
import datetime
from playwright.sync_api import sync_playwright

def run(playwright):
    browser = playwright.chromium.launch(headless=True)
    page = browser.new_page()

    # Dynamic date for "Today"
    now = datetime.datetime.now(datetime.timezone.utc)
    today_iso = now.strftime("%Y-%m-%dT%H:%M:%SZ")

    # Mock API responses
    clips_data = [
        {
            "ID": 101,
            "timestamp": today_iso,
            "event": "Sentry",
            "city": "Austin",
            "reason": "sentry_aware_object_detection",
            "video_files": [
                {"camera": "Front", "file_path": "/test/front.mp4", "timestamp": today_iso}
            ],
            "telemetry": {}
        },
        {
            "ID": 102,
            "timestamp": today_iso,
            "event": "Saved",
            "city": "Austin",
            "reason": "user_interaction_dashcam_icon_tapped",
            "video_files": [
                {"camera": "Front", "file_path": "/test/front2.mp4", "timestamp": today_iso}
            ],
            "telemetry": {}
        }
    ]

    # Mock /api/clips
    page.route("**/api/clips", lambda route: route.fulfill(
        status=200,
        body=json.dumps(clips_data),
        headers={"Content-Type": "application/json"}
    ))

    # Mock /api/version
    page.route("**/api/version", lambda route: route.fulfill(
        status=200,
        body=json.dumps([]),
        headers={"Content-Type": "application/json"}
    ))

    # Mock /api/clips/101
    page.route("**/api/clips/101", lambda route: route.fulfill(
        status=200,
        body=json.dumps(clips_data[0]),
        headers={"Content-Type": "application/json"}
    ))

    # Navigate to app
    page.goto("http://localhost:5173")

    # Wait for sidebar to load clips
    # "Sentry Event" should be visible
    page.wait_for_selector("text=Sentry Event")

    # Verify filtering works (click filter button)
    page.get_by_role("button", name="Show filters").click()

    # Check "Sentry Object" filter is checked
    checkbox = page.get_by_label("Object Detection")
    if checkbox.is_checked():
        print("Filter checkbox is checked as expected")

    # Take screenshot of sidebar with filters open
    page.screenshot(path=".jules/verification/sidebar_optimization.png")

    browser.close()

with sync_playwright() as playwright:
    run(playwright)
