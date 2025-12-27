import time
from playwright.sync_api import sync_playwright

def verify_timeline_accessibility():
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        # Create a new context with a larger viewport
        context = browser.new_context(viewport={'width': 1280, 'height': 800})
        page = context.new_page()

        # Mock API responses to allow the Player to render
        page.route("**/api/version", lambda route: route.fulfill(
            status=200,
            content_type="application/json",
            body='{"latestVersion": "v1.0.0", "releases": []}'
        ))

        # Mock Clips list
        page.route("**/api/clips", lambda route: route.fulfill(
            status=200,
            content_type="application/json",
            body='''
            [
                {
                    "ID": 1,
                    "timestamp": "2023-10-27T10:00:00Z",
                    "event": "Sentry",
                    "city": "Test City",
                    "video_files": [
                        {"camera": "Front", "file_path": "/test/front.mp4", "timestamp": "2023-10-27T10:00:00Z"}
                    ],
                    "telemetry": {}
                }
            ]
            '''
        ))

        # Mock Clip details
        page.route("**/api/clips/1", lambda route: route.fulfill(
            status=200,
            content_type="application/json",
            body='''
            {
                "ID": 1,
                "timestamp": "2023-10-27T10:00:00Z",
                "event": "Sentry",
                "city": "Test City",
                "video_files": [
                    {"camera": "Front", "file_path": "/test/front.mp4", "timestamp": "2023-10-27T10:00:00Z"}
                ],
                "telemetry": {}
            }
            '''
        ))

        # Mock Thumbnail
        page.route("**/api/thumbnail**", lambda route: route.fulfill(
            status=200,
            content_type="image/svg+xml",
            body='<svg width="100" height="100" xmlns="http://www.w3.org/2000/svg"><rect width="100" height="100" fill="gray"/></svg>'
        ))

        # Navigate to the app
        print("Navigating to app...")
        page.goto("http://localhost:5173/")

        # Wait for the app to load and player to appear
        # The app automatically selects the first clip, so the player should render
        print("Waiting for player...")
        try:
            page.wait_for_selector('role=slider', timeout=10000)
        except Exception as e:
            print("Timeout waiting for slider. Dumping page content...")
            print(page.content())
            page.screenshot(path="verification_timeout.png")
            raise e

        # Get the slider element
        slider = page.get_by_role("slider")

        # Check for aria-orientation
        aria_orientation = slider.get_attribute("aria-orientation")
        print(f"aria-orientation: {aria_orientation}")

        if aria_orientation != "horizontal":
            print("FAIL: aria-orientation is missing or incorrect")
        else:
            print("PASS: aria-orientation is correct")

        # Focus the slider to trigger focus styles
        slider.focus()

        # Take a screenshot to verify handle visibility (though hard to assert programmatically without image diff)
        page.screenshot(path="verification_scrubber.png")
        print("Screenshot saved to verification_scrubber.png")

        # Check for the handle class update by inspecting the HTML
        # We need to find the handle div inside the slider. It's the one with transition-transform and scale classes.
        # The handle is the 2nd child of the slider div in the current structure:
        # 1. Track Background
        # 2. Handle
        # 3. Markers

        # Let's get the inner HTML of the slider to inspect
        slider_html = slider.inner_html()
        print("Slider Inner HTML:", slider_html)

        if "group-focus-visible:scale-100" in slider_html:
            print("PASS: group-focus-visible:scale-100 class found")
        else:
            print("FAIL: group-focus-visible:scale-100 class NOT found")

        browser.close()

if __name__ == "__main__":
    verify_timeline_accessibility()
