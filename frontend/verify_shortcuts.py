
import json
from playwright.sync_api import sync_playwright, expect

def test_keyboard_shortcuts():
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        page = browser.new_page()

        # Console logging
        page.on("console", lambda msg: print(f"CONSOLE: {msg.text}"))
        page.on("pageerror", lambda err: print(f"PAGE ERROR: {err}"))

        # Mock APIs
        # Note: glob pattern matching might be tricky if base url differs.
        # We'll use regex or broader glob.

        def handle_clips(route):
            print("Handling /api/clips")
            route.fulfill(
                status=200,
                content_type="application/json",
                body=json.dumps([
                    {
                        "ID": 123,
                        "timestamp": "2023-10-27T10:00:00Z",
                        "event": "Sentry",
                        "city": "Test City",
                        "video_files": [
                            {"camera": "Front", "file_path": "/test_front.mp4", "timestamp": "2023-10-27T10:00:00Z"}
                        ]
                    }
                ])
            )

        page.route("**/api/clips", handle_clips)

        page.route("**/api/clips/123", lambda route: route.fulfill(
            status=200,
            content_type="application/json",
            body=json.dumps({
                "ID": 123,
                "timestamp": "2023-10-27T10:00:00Z",
                "event": "Sentry",
                "city": "Test City",
                "video_files": [
                    {"camera": "Front", "file_path": "/test_front.mp4", "timestamp": "2023-10-27T10:00:00Z"}
                ],
                "telemetry": {}
            })
        ))

        page.route("**/api/transcode/status", lambda route: route.fulfill(
             status=200,
             body=json.dumps({"encoder": "test_enc", "hw_accel": False})
        ))

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
            page.screenshot(path="frontend_failed.png")
            raise e

        # Wait for Player component controls
        print("Waiting for Play button...")
        play_button = page.get_by_title("Play (Space)")
        expect(play_button).to_be_visible()

        # Test Space to Play
        print("Pressing Space...")
        page.keyboard.press("Space")

        # Expect button to change to Pause
        print("Waiting for Pause button...")
        pause_button = page.get_by_title("Pause (Space)")
        expect(pause_button).to_be_visible()

        # Test Space to Pause
        print("Pressing Space again...")
        page.keyboard.press("Space")
        expect(play_button).to_be_visible()

        # Test Tooltip
        # Hover over Rewind button
        print("Hovering Rewind...")
        rewind_button = page.get_by_label("Rewind 15 seconds")
        rewind_button.hover()

        # Take screenshot of tooltip
        page.screenshot(path="verification_shortcuts.png")

        print("Verification successful!")
        browser.close()

if __name__ == "__main__":
    test_keyboard_shortcuts()
