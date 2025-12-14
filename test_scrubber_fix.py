from playwright.sync_api import sync_playwright
import json
import time

def run(playwright):
    browser = playwright.chromium.launch(headless=True)
    page = browser.new_page(viewport={"width": 1280, "height": 720})

    # Mock Data
    clip_id = 1
    # Invalid timestamp string causing NaN
    mock_clip = {
        "ID": clip_id,
        "timestamp": "InvalidDateString",
        "event": "Recent",
        "city": "Test City",
        "video_files": [
            {
                "camera": "Front",
                "file_path": "front.mp4",
                "timestamp": "InvalidDateString" # Should result in NaN -> 0
            },
             {
                "camera": "Front",
                "file_path": "front2.mp4",
                "timestamp": "2024-01-01T00:00:00Z" # Valid
            }
        ],
        "telemetry": {}
    }

    # Mock /api/clips
    def handle_clips(route):
        route.fulfill(
            status=200,
            content_type="application/json",
            body=json.dumps([mock_clip])
        )
    page.route("**/api/clips", handle_clips)

    # Mock /api/clips/1
    def handle_clip_detail(route):
        route.fulfill(
            status=200,
            content_type="application/json",
            body=json.dumps(mock_clip)
        )
    page.route(f"**/api/clips/{clip_id}", handle_clip_detail)

    # Mock video file (empty)
    page.route("**/api/video/**", lambda route: route.fulfill(
        status=200,
        content_type="video/mp4",
        headers={"Content-Range": "bytes 0-100/1000"},
        body=b"fakevideodata"
    ))

    page.route("**/api/thumbnail/**", lambda route: route.fulfill(status=404))

    # Mock version
    page.route("**/api/version", lambda route: route.fulfill(
        status=200,
        content_type="application/json",
        body=json.dumps([])
    ))

    print("Navigating...")
    page.goto("http://localhost:5173")

    # Wait for player to load
    try:
        page.wait_for_selector("text=Test City", timeout=10000)
        print("Loaded clip.")
    except:
        print("Timeout waiting for clip.")

    # Check timeline text
    # It should say "0:00" or similar
    # Screenshot the player controls

    time.sleep(2) # Wait for renders

    page.screenshot(path="verification_scrubber.png")
    print("Screenshot saved to verification_scrubber.png")

    browser.close()

if __name__ == "__main__":
    with sync_playwright() as playwright:
        run(playwright)
