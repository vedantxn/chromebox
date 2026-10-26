"""The clients against a fake API (httpx.MockTransport): no network."""

import json

import httpx
import pytest

from batonvm import (AsyncBaton, AsyncSession, AuthError, Baton, BatonError, CapacityError, LimitError,
                     NotFoundError, Session)

SESSION = {"id": "vm-1", "status": "running", "node": "node-a", "createdAt": "2026-10-26T00:00:00Z",
           "expiresAt": "2026-10-26T00:15:00Z", "migrations": 0, "connectUrl": "wss://x/cdp/t1", "newField": 7}


class FakeAPI:
    """Answers by (method, path); records every request."""

    def __init__(self, routes):
        self.routes, self.calls = routes, []

    def __call__(self, req: httpx.Request) -> httpx.Response:
        body = json.loads(req.content) if req.content else None
        path = req.url.raw_path.decode().split("?")[0]  # as sent, still percent-encoded
        self.calls.append((req.method, path, body, req.headers.get("authorization")))
        r = self.routes[(req.method, path)]
        r = r.pop(0) if isinstance(r, list) else r
        status, data, *headers = r
        return httpx.Response(status, json=data, headers=headers[0] if headers else None) if data is not None else httpx.Response(status)


def sync_client(routes):
    api = FakeAPI(routes)
    return Baton("bk_test", "https://api.test", http_client=httpx.Client(transport=httpx.MockTransport(api))), api


def async_client(routes):
    api = FakeAPI(routes)
    return AsyncBaton("bk_test", "https://api.test", http_client=httpx.AsyncClient(transport=httpx.MockTransport(api))), api


def test_needs_a_key(monkeypatch):
    monkeypatch.delenv("BATON_API_KEY", raising=False)
    with pytest.raises(AuthError, match="BATON_API_KEY"):
        Baton()


def test_reads_key_and_base_url_from_the_environment(monkeypatch):
    monkeypatch.setenv("BATON_API_KEY", "bk_env")
    monkeypatch.setenv("BATON_BASE_URL", "https://other.test/")
    b = Baton()
    assert b._key == "bk_env" and b._base == "https://other.test"


def test_create_maps_the_session_and_keeps_unknown_fields():
    b, api = sync_client({("POST", "/api/v1/sessions"): (201, SESSION)})
    s = b.sessions.create(profile="shop")
    assert isinstance(s, Session)
    assert (s.id, s.node, s.connect_url, s.expires_at) == ("vm-1", "node-a", "wss://x/cdp/t1", "2026-10-26T00:15:00Z")
    assert s.raw["newField"] == 7
    assert api.calls == [("POST", "/api/v1/sessions", {"profile": "shop"}, "Bearer bk_test")]


def test_session_methods_hit_their_endpoints():
    b, api = sync_client({
        ("POST", "/api/v1/sessions"): (201, SESSION),
        ("POST", "/api/v1/sessions/vm-1/migrate"): (200, {"from": "node-a", "to": "node-b", "pausedMs": 212, "unreachableMs": 214}),
        ("POST", "/api/v1/sessions/vm-1/fork"): (200, {"parent": "vm-1", "pausedMs": 210, "sessions": [dict(SESSION, id="vm-2")], "errors": []}),
        ("POST", "/api/v1/sessions/vm-1/pause"): (200, {"status": "paused", "pausedMs": 457, "totalMs": 1870, "storedMiB": 611.3}),
        ("POST", "/api/v1/sessions/vm-1/resume"): (200, dict(SESSION, node="node-c", connectUrl="wss://x/cdp/t2")),
        ("POST", "/api/v1/profiles"): (201, {"name": "shop", "pausedMs": 217, "uploadMs": 1400, "storedMiB": 602}),
        ("POST", "/api/v1/sessions/vm-1/live"): (200, {"url": "wss://x/cdp/view"}),
        ("GET", "/api/v1/sessions/vm-1/events"): (200, [{"at": "t", "kind": "created", "detail": {"warm": True}}]),
        ("DELETE", "/api/v1/sessions/vm-1"): (204, None),
    })
    s = b.sessions.create()
    m = s.migrate()
    assert (m.from_node, m.to_node, m.paused_ms, m.unreachable_ms) == ("node-a", "node-b", 212, 214)
    f = s.fork(1)
    assert f.paused_ms == 210 and [c.id for c in f.sessions] == ["vm-2"] and isinstance(f.sessions[0], Session)
    p = s.pause()
    assert (p.paused_ms, p.total_ms, p.stored_mib) == (457, 1870, 611.3)
    assert s.resume() is s and s.connect_url == "wss://x/cdp/t2" and s.node == "node-c"
    assert s.save_profile("shop").stored_mib == 602
    assert s.live_url() == "wss://x/cdp/view"
    assert s.events()[0].detail == {"warm": True}
    s.close()
    assert ("POST", "/api/v1/profiles", {"session": "vm-1", "name": "shop"}, "Bearer bk_test") in api.calls
    assert api.calls[-1][:2] == ("DELETE", "/api/v1/sessions/vm-1")


def test_profiles_and_usage():
    b, api = sync_client({
        ("GET", "/api/v1/profiles"): (200, [{"name": "my shop", "createdAt": "t", "storedMiB": 744.6}]),
        ("DELETE", "/api/v1/profiles/my%20shop"): (204, None),
        ("GET", "/api/v1/usage"): (200, {"running": 1, "max_running": 2, "paused": 0, "max_paused": 5, "profiles": 1,
                                         "max_profiles": 3, "minutes_today": 3.2, "max_minutes_per_day": 60,
                                         "session_lifetime_minutes": 15}),
        ("GET", "/api/v1/sessions"): (200, [SESSION]),
        ("GET", "/api/v1/sessions/vm-1"): (200, SESSION),
    })
    assert b.profiles.list()[0].name == "my shop"
    b.profiles.delete("my shop")
    u = b.usage()
    assert (u.running, u.max_running, u.minutes_today) == (1, 2, 3.2)
    assert [s.id for s in b.sessions.list()] == ["vm-1"] and b.sessions.get("vm-1").status == "running"


@pytest.mark.parametrize("status,cls", [(401, AuthError), (404, NotFoundError), (429, LimitError), (503, CapacityError), (500, BatonError)])
def test_errors_map_to_their_class(status, cls):
    b, _ = sync_client({("GET", "/api/v1/sessions/vm-9"): (status, {"error": "nope"})})
    with pytest.raises(cls) as e:
        b.sessions.get("vm-9")
    assert e.value.status == status and e.value.message == "nope"
    assert type(e.value) is cls


def test_create_waits_for_capacity_then_succeeds():
    b, api = sync_client({("POST", "/api/v1/sessions"): [(503, {"error": "starting a machine"}, {"Retry-After": "30"}),
                                                       (503, {"error": "starting a machine"}, {"Retry-After": "5"}),
                                                       (201, SESSION)]})
    slept = []
    b._sleep = slept.append
    assert b.sessions.create().id == "vm-1"
    assert slept == [15.0, 5.0]  # never more than 15 s at a time


def test_capacity_wait_gives_up_after_three_minutes():
    b, _ = sync_client({("POST", "/api/v1/sessions"): (503, {"error": "full"}, {"Retry-After": "15"})})
    clock = [0.0]
    b._now = lambda: clock[0]
    b._sleep = lambda s: clock.__setitem__(0, clock[0] + s)
    with pytest.raises(CapacityError):
        b.sessions.create()
    assert 180 <= clock[0] <= 200


def test_other_calls_do_not_wait_for_capacity():
    b, api = sync_client({("POST", "/api/v1/sessions/vm-1/migrate"): (503, {"error": "full"}, {"Retry-After": "15"})})
    b._sleep = lambda s: pytest.fail("migrate must not wait")
    with pytest.raises(CapacityError):
        b.sessions.migrate("vm-1")
    assert len(api.calls) == 1


def test_context_manager_closes_and_keeps_the_original_error():
    b, api = sync_client({("POST", "/api/v1/sessions"): (201, SESSION), ("DELETE", "/api/v1/sessions/vm-1"): (500, {"error": "x"})})
    with pytest.raises(ValueError, match="mine"):
        with b.sessions.create():
            raise ValueError("mine")  # the DELETE fails too, but this is the error that surfaces
    assert api.calls[-1][:2] == ("DELETE", "/api/v1/sessions/vm-1")


async def test_async_client():
    b, api = async_client({
        ("POST", "/api/v1/sessions"): [(503, {"error": "starting"}, {"Retry-After": "2"}), (201, SESSION)],
        ("POST", "/api/v1/sessions/vm-1/migrate"): (200, {"from": "node-a", "to": "node-b", "pausedMs": 212, "unreachableMs": 214}),
        ("POST", "/api/v1/sessions/vm-1/fork"): (200, {"parent": "vm-1", "pausedMs": 210, "sessions": [dict(SESSION, id="vm-2")]}),
        ("POST", "/api/v1/sessions/vm-1/resume"): (200, dict(SESSION, connectUrl="wss://x/cdp/t2")),
        ("DELETE", "/api/v1/sessions/vm-1"): (204, None),
    })
    slept = []

    async def sleep(s):
        slept.append(s)

    b._sleep = sleep
    async with b:
        async with await b.sessions.create() as s:
            assert isinstance(s, AsyncSession) and slept == [2.0]
            assert (await s.migrate()).to_node == "node-b"
            assert isinstance((await s.fork()).sessions[0], AsyncSession)
            await s.resume()
            assert s.connect_url == "wss://x/cdp/t2"
    assert api.calls[-1][:2] == ("DELETE", "/api/v1/sessions/vm-1")
