from playwright.sync_api import sync_playwright, expect

def run(playwright):
    browser = playwright.chromium.launch(headless=True)
    page = browser.new_page()

    # Mock API
    page.route("**/api/clips", lambda route: route.fulfill(
        status=200,
        content_type="application/json",
        body='''[
            {
                "ID": 1,
                "timestamp": "2023-10-27T10:00:00Z",
                "event": "Saved",
                "city": "New York",
                "telemetry": { "latitude": 40.7128, "longitude": -74.0060 },
                "video_files": [{ "camera": "Front", "file_path": "/test1.mp4" }]
            },
            {
                "ID": 2,
                "timestamp": "2023-10-27T12:00:00Z",
                "event": "Sentry",
                "city": "Los Angeles",
                "telemetry": { "latitude": 34.0522, "longitude": -118.2437 },
                "video_files": [{ "camera": "Front", "file_path": "/test2.mp4" }]
            }
        ]'''
    ))

    # Mock Version
    page.route("**/api/version", lambda route: route.fulfill(
        status=200,
        content_type="application/json",
        body='{"version": "0.1.6", "release_date": "2023-12-13", "content": []}'
    ))

    # Mock Thumbnails
    # Return a red pixel
    red_pixel = b'\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x02\x00\x00\x00\x90wS\xde\x00\x00\x00\x0cIDAT\x08\xd7c\xf8\xcf\xc0\x00\x00\x03\x01\x01\x00\x18\xdd\x8d\xb0\x00\x00\x00\x00IEND\xaeB`\x82'
    page.route("**/api/thumbnail*", lambda route: route.fulfill(
        status=200,
        content_type="image/png",
        body=red_pixel
    ))

    page.goto("http://localhost:5173")

    # Click Map Button
    # The title is "View Map"
    page.get_by_title("View Map").click()

    # Wait for MapModal
    # It has a "Close Map" button
    expect(page.get_by_label("Close Map")).to_be_visible()

    # Wait for markers.
    # Leaflet renders markers as images.
    # We can wait for the popup content if we click a marker, but for visual verification of bounds,
    # simply seeing the map with 2 markers is enough.
    # The map should auto-fit.
    # We wait a bit for the animation.
    page.wait_for_timeout(2000)

    page.screenshot(path="verification/map_modal.png")
    browser.close()

with sync_playwright() as playwright:
    run(playwright)
