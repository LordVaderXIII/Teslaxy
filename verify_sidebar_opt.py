from playwright.sync_api import sync_playwright, expect
import datetime
import json
import os

def run(playwright):
    browser = playwright.chromium.launch(headless=True)
    context = browser.new_context(viewport={"width": 1280, "height": 720})
    page = context.new_page()

    # Mock API
    today_iso = datetime.date.today().isoformat() # YYYY-MM-DD

    # Create mock clips
    clips = [
        {
            "ID": 1,
            "timestamp": f"{today_iso}T10:00:00Z",
            "event": "Sentry",
            "city": "San Francisco",
            "reason": "sentry_aware_object_detection",
            "video_files": [
                {"camera": "Front", "file_path": "/front.mp4", "timestamp": f"{today_iso}T10:00:00Z"}
            ],
            "telemetry": {}
        },
        {
            "ID": 2,
            "timestamp": f"{today_iso}T10:05:00Z",
            "event": "Saved",
            "city": "San Francisco",
            "reason": "user_interaction_dashcam_icon_tapped",
            "video_files": [
                {"camera": "Front", "file_path": "/front2.mp4", "timestamp": f"{today_iso}T10:05:00Z"}
            ],
            "telemetry": {}
        }
    ]

    page.route("**/api/clips", lambda route: route.fulfill(
        status=200,
        content_type="application/json",
        body=json.dumps(clips)
    ))

    page.route("**/api/transcode/status", lambda route: route.fulfill(
        status=200,
        content_type="application/json",
        body=json.dumps({"encoder": "cpu", "hw_accel": False})
    ))

    page.route("**/api/version", lambda route: route.fulfill(
        status=200,
        content_type="application/json",
        body=json.dumps({"version": "v1.0.0"})
    ))

    # Mock thumbnail to avoid errors
    page.route("**/api/thumbnail**", lambda route: route.fulfill(
        status=200,
        content_type="image/png",
        body=b""
    ))

    # Go to app
    page.goto("http://localhost:4173")

    # Verification
    # Check if Sidebar loads and displays clips
    # We expect "San Francisco" to be visible
    expect(page.get_by_text("San Francisco").first).to_be_visible()

    # Check if dates are displayed correctly (e.g. 10:00 AM)
    # Depending on locale, might be 10:00 AM or 10:00
    # toLocaleTimeString([], {hour: '2-digit', minute:'2-digit'})
    # We can check for "10:00"
    expect(page.get_by_text("10:00").first).to_be_visible()

    # Take screenshot
    os.makedirs("/home/jules/verification", exist_ok=True)
    page.screenshot(path="/home/jules/verification/sidebar_opt.png")

    print("Verification passed!")

    browser.close()

with sync_playwright() as playwright:
    run(playwright)
