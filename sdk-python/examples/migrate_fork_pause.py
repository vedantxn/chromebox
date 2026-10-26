"""Everything a session can do: move it, copy it, put it to sleep, save it as a profile."""
import time

from batonvm import Baton
from playwright.sync_api import sync_playwright

t0 = time.monotonic()


def lap(what):
    print(f"{(time.monotonic() - t0) * 1000:7.0f} ms  {what}")


baton = Baton()
with sync_playwright() as p:
    session = baton.sessions.create()
    lap(f"created {session.id} on {session.node}")
    browser = p.chromium.connect_over_cdp(session.connect_url)
    page = browser.contexts[0].pages[0]
    page.goto("https://example.com")
    lap(f"loaded: {page.title()}")

    move = session.migrate()
    lap(f"moved {move.from_node} → {move.to_node}, paused {move.paused_ms} ms; still {page.title()}")

    copy = session.fork(1).sessions[0]  # same pages, cookies, storage
    twin = p.chromium.connect_over_cdp(copy.connect_url)
    lap(f"forked into {copy.id}: {[pg.url for pg in twin.contexts[0].pages]}")
    copy.close()

    paused = session.pause()  # stored; no machine holds it
    lap(f"paused: {paused.stored_mib} MiB stored")
    session.resume()  # back on any machine, with a fresh connect_url
    back = p.chromium.connect_over_cdp(session.connect_url)
    lap(f"resumed on {session.node}: {[pg.url for pg in back.contexts[0].pages]}")

    session.save_profile("shop")  # log in once...
    later = baton.sessions.create(profile="shop")  # ...start already logged in
    lap(f"started {later.id} from the profile")

    print("events:", [e.kind for e in session.events()])
    print("usage:", baton.usage().running, "running,", round(baton.usage().minutes_today, 1), "minutes today")
    session.close()
    later.close()
    baton.profiles.delete("shop")
    lap("closed both, profile deleted")
