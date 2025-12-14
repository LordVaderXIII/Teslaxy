from playwright.sync_api import sync_playwright
import json
import time

def run(playwright):
    browser = playwright.chromium.launch(headless=True)
    page = browser.new_page(viewport={"width": 1280, "height": 720})

    clip_id = 1
    mock_clip = {
        "ID": clip_id,
        "timestamp": "InvalidDateString",
        "event": "Recent",
        "city": "Test City",
        "video_files": [
            {
                "camera": "Front",
                "file_path": "front.mp4",
                "timestamp": "InvalidDateString"
            }
        ],
        "telemetry": {}
    }

    page.route("**/api/clips", lambda route: route.fulfill(status=200, content_type="application/json", body=json.dumps([mock_clip])))
    page.route(f"**/api/clips/{clip_id}", lambda route: route.fulfill(status=200, content_type="application/json", body=json.dumps(mock_clip)))
    page.route("**/api/video/**", lambda route: route.fulfill(status=200, body=b""))
    page.route("**/api/thumbnail/**", lambda route: route.fulfill(status=404))
    page.route("**/api/version", lambda route: route.fulfill(status=200, body=json.dumps([])))

    page.goto("http://localhost:5173")
    time.sleep(2)
    page.screenshot(path="verification_all_invalid.png")
    browser.close()

if __name__ == "__main__":
    with sync_playwright() as playwright:
        run(playwright)
