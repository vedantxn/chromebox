"""The same with asyncio, as browser-use and Playwright's async API use it."""
import asyncio

from batonvm import AsyncBaton
from playwright.async_api import async_playwright


async def main():
    async with AsyncBaton() as baton, async_playwright() as p:
        async with await baton.sessions.create() as session:
            browser = await p.chromium.connect_over_cdp(session.connect_url)
            page = browser.contexts[0].pages[0]
            await page.goto("https://example.com")
            print(await page.title())

            move = await session.migrate()
            print(f"moved {move.from_node} → {move.to_node}, paused {move.paused_ms} ms")
            print(await page.title())


asyncio.run(main())
