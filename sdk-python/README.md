# batonvm

Cloud browsers for agents, from Python. Every session is a real Chrome in its own Firecracker microVM, started in about 200 ms. A running session can **move to another machine** while your CDP connection stays open, be **forked** into copies that are logged in the same way, and be **paused** to storage and resumed later.

## Quickstart

1. Sign in at **https://batonvm.vercel.app** (email or GitHub) and create an API key.
2. Install:

```sh
pip install batonvm playwright
export BATON_API_KEY=bk_...
```

Playwright only connects to the remote browser, so it needs no local browser download.

3. Run:

```python
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
```

The session ends when the `with` block does.

### Async (browser-use, Playwright's async API)

```python
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

asyncio.run(main())
```

## Move it, copy it, put it to sleep

```python
copies = session.fork(3)             # 3 new sessions: same pages, cookies, storage
for copy in copies.sessions:
    print(copy.id, copy.connect_url)

session.pause()                      # stored; no machine holds it; up to 7 days
session.resume()                     # back on any machine; session.connect_url is new

session.save_profile("shop")         # log in once...
later = baton.sessions.create(profile="shop")  # ...start already logged in
```

## Options

The browser has these before you connect, so the first page already sees them:

```python
session = baton.sessions.create(
    viewport=(1280, 720),            # or {"width": 1280, "height": 720}
    user_agent="MyAgent/1.0",
    locale="fr-FR",                  # navigator.language and Accept-Language
    timezone="Europe/Paris",         # any IANA name
    timeout_minutes=30,              # 1-60, default 15
    proxy={"server": "proxy.example.com:8080", "username": "u", "password": "p"},  # or "host:port"
)
print(session.options)               # what it started with; a proxy password shows as ********
```

All are optional and work with `profile=`. Forks and resumed sessions keep them. A value the API won't take raises `BatonError` with status 400 and says why.

## API

| | |
|---|---|
| `Baton(api_key=None, base_url=None)` | client; `api_key` defaults to `BATON_API_KEY` (`AsyncBaton` for asyncio) |
| `baton.sessions.create(profile=None, *, viewport, user_agent, locale, timezone, timeout_minutes, proxy)` | start a browser → `Session` with `connect_url` and `options` |
| `baton.sessions.list()` / `get(id)` / `delete(id)` | |
| `session.migrate()` | move it to another machine → `Migration(from_node, to_node, paused_ms, unreachable_ms)` |
| `session.fork(n)` | copy it into `n` new sessions → `Fork(sessions, paused_ms, errors)` |
| `session.pause()` / `session.resume()` | store it and free its machine; bring it back, with a fresh `connect_url` |
| `session.save_profile(name)` | save its pages, cookies and storage; `sessions.create(profile=name)` starts from it |
| `baton.profiles.list()` / `delete(name)` | |
| `session.events()` | what happened to it: created, migrated, forked, paused, ended and why |
| `session.live_url()` | a short-lived URL for watching it |
| `session.close()` | end it (or use the session as a context manager) |
| `baton.usage()` | running sessions and minutes used today, with your limits |

Errors are `BatonError` with `.status`, raised as `AuthError` (401), `NotFoundError` (404), `LimitError` (429) or `CapacityError` (503). When every machine is full and a new one is starting, `sessions.create()` and `resume()` wait for it (about a minute) instead of failing.

## Limits (free)

2 sessions running at a time · 15 minutes per session (up to 60 with `timeout_minutes`) · 60 browser-minutes per day · 10 new sessions per minute · 5 paused sessions (kept 7 days) · 3 profiles.

Source: https://github.com/vedantxn/chromebox (MIT). TypeScript: `npm install batonvm`.
