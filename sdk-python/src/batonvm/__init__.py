"""Baton: cloud browsers for agents. Each session is a real Chrome in its own
microVM that can move between machines, be forked, and be paused to storage.

    from batonvm import Baton
    from playwright.sync_api import sync_playwright

    baton = Baton()  # reads BATON_API_KEY
    with baton.sessions.create() as session, sync_playwright() as p:
        browser = p.chromium.connect_over_cdp(session.connect_url)
        page = browser.contexts[0].pages[0]
        page.goto("https://example.com")
        move = session.migrate()  # another machine; `page` keeps working
"""

from ._client import AsyncBaton, AsyncSession, Baton, Fork, Session
from ._errors import AuthError, BatonError, CapacityError, LimitError, NotFoundError
from ._types import Event, Migration, Pause, ProfileInfo, SavedProfile, Usage

__version__ = "0.1.0"

__all__ = [
    "Baton", "AsyncBaton", "Session", "AsyncSession", "Fork",
    "Migration", "Pause", "ProfileInfo", "SavedProfile", "Event", "Usage",
    "BatonError", "AuthError", "NotFoundError", "LimitError", "CapacityError",
    "__version__",
]
