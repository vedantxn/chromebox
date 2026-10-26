"""The README quickstart: a browser, a page, and a move to another machine mid-script."""
from batonvm import Baton
from playwright.sync_api import sync_playwright

baton = Baton()  # reads BATON_API_KEY

with baton.sessions.create() as session, sync_playwright() as p:
    browser = p.chromium.connect_over_cdp(session.connect_url)
    page = browser.contexts[0].pages[0]
    page.goto("https://example.com")
    print(page.title())

    move = session.migrate()  # the browser changes machines mid-script
    print(f"moved {move.from_node} → {move.to_node}, paused {move.paused_ms} ms")
    print(page.title())  # same page, same connection
