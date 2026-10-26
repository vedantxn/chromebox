"""The Baton clients: ``Baton`` (sync) and ``AsyncBaton`` (async), over the same
REST API as the TypeScript SDK. Each session is a real Chrome in its own
microVM; connect to ``session.connect_url`` with Playwright over CDP."""

from __future__ import annotations

import asyncio
import os
import time
from dataclasses import dataclass, field
from typing import Any, Callable, Dict, List, Mapping, Optional, Sequence, Union
from urllib.parse import quote

import httpx

from ._errors import AuthError, error_for
from ._types import Event, Migration, Pause, ProfileInfo, SavedProfile, SessionData, Usage, errors_of, session_fields

DEFAULT_BASE_URL = "https://batonvm.vercel.app"
# When every machine is full and one is being started, the API answers 503
# with Retry-After. Creating or resuming a session waits for it (up to 3
# minutes, at most 15 s between tries) instead of failing: the new machine is
# up in about a minute.
CAPACITY_WAIT_S = 180.0
MAX_SLEEP_S = 15.0
# A fork, a pause or saving a profile takes a few seconds; httpx's default 5 s is too short.
TIMEOUT = httpx.Timeout(120.0, connect=15.0)


def _settings(api_key: Optional[str], base_url: Optional[str]) -> "tuple[str, str]":
    key = api_key or os.environ.get("BATON_API_KEY", "")
    if not key:
        raise AuthError("No API key: pass api_key= or set BATON_API_KEY (create one at https://batonvm.vercel.app/dashboard)", 401)
    url = (base_url or os.environ.get("BATON_BASE_URL") or DEFAULT_BASE_URL).rstrip("/")
    return key, url


def _retry_in(res: httpx.Response, wait: bool, give_up: float, now: float) -> Optional[float]:
    """Seconds to wait before trying again, or None to stop."""
    if res.status_code != 503 or not wait or now > give_up:
        return None
    try:
        after = float(res.headers.get("retry-after", ""))
    except ValueError:
        return None
    return min(after, MAX_SLEEP_S) if after > 0 else None


def _result(res: httpx.Response) -> Any:
    if res.status_code == 204:
        return None
    try:
        data = res.json()
    except ValueError:
        data = {}
    if res.status_code >= 400:
        msg = data.get("error") if isinstance(data, dict) else None
        raise error_for(res.status_code, msg or res.reason_phrase or f"HTTP {res.status_code}")
    return data


Viewport = Union[Sequence[int], Mapping[str, int]]
Proxy = Union[str, Mapping[str, Optional[str]]]


def _create_body(profile: Optional[str], viewport: Optional[Viewport], user_agent: Optional[str], locale: Optional[str],
                 timezone: Optional[str], timeout_minutes: Optional[int], proxy: Optional[Proxy]) -> Optional[Dict[str, Any]]:
    """The create request, in the API's camelCase. The API checks the values (400 if one is wrong)."""
    body: Dict[str, Any] = {}
    if profile:
        body["profile"] = profile
    if viewport is not None:
        if isinstance(viewport, Mapping):
            body["viewport"] = {"width": viewport["width"], "height": viewport["height"]}
        else:
            width, height = viewport
            body["viewport"] = {"width": width, "height": height}
    if user_agent is not None:
        body["userAgent"] = user_agent
    if locale is not None:
        body["locale"] = locale
    if timezone is not None:
        body["timezone"] = timezone
    if timeout_minutes is not None:
        body["timeoutMinutes"] = timeout_minutes
    if proxy is not None:
        body["proxy"] = {"server": proxy} if isinstance(proxy, str) else {k: v for k, v in proxy.items() if v is not None}
    return body or None


_CREATE_DOC = """Start a browser.

        profile: a saved profile to start from (already logged in, on the pages it was saved on).
        viewport: ``(width, height)`` or ``{"width": ..., "height": ...}`` (320-3840 x 240-2160).
        user_agent: sent in requests and reported by ``navigator.userAgent``.
        locale: for example ``"fr-FR"`` (``navigator.language``, Accept-Language).
        timezone: an IANA name such as ``"Europe/Paris"`` or ``"UTC"``.
        timeout_minutes: how long the session may run (1-60, default 15).
        proxy: an HTTP proxy of your own, with CONNECT: ``"host:port"`` or
            ``{"server": "host:port", "username": ..., "password": ...}``. Every page's traffic leaves through it.

        The browser has all of these before you connect."""


@dataclass
class Fork:
    """A running browser copied into new sessions: same pages, cookies and storage."""

    parent: str
    #: How long the parent was paused while its snapshot was taken.
    paused_ms: int
    #: The copies, each with its own ``connect_url``.
    sessions: List[Any]
    #: Copies that could not be started (for example, over a limit).
    errors: List[str] = field(default_factory=list)
    raw: Dict[str, Any] = field(default_factory=dict, repr=False, compare=False)


# ---------------------------------------------------------------- sync


@dataclass
class Session(SessionData):
    """A browser session. Use it as a context manager to close it on exit::

    with baton.sessions.create() as session:
        browser = playwright.chromium.connect_over_cdp(session.connect_url)
    """

    _client: Optional["Baton"] = field(default=None, repr=False, compare=False)

    def close(self) -> None:
        """End the session and destroy its browser."""
        self._client.sessions.delete(self.id)  # type: ignore[union-attr]

    def migrate(self) -> Migration:
        """Move the running browser to another machine. Connections stay open."""
        return self._client.sessions.migrate(self.id)  # type: ignore[union-attr]

    def fork(self, count: int = 1) -> Fork:
        """Copy this running browser into ``count`` new sessions (same pages, cookies, storage)."""
        return self._client.sessions.fork(self.id, count)  # type: ignore[union-attr]

    def pause(self) -> Pause:
        """Store this browser and free its machine. Open connections close; ``resume()`` within 7 days."""
        return self._client.sessions.pause(self.id)  # type: ignore[union-attr]

    def resume(self) -> "Session":
        """Bring a paused browser back, on any machine. Updates ``connect_url``; returns this session."""
        self._update(self._client._request("POST", f"/api/v1/sessions/{self.id}/resume", wait_for_capacity=True))  # type: ignore[union-attr]
        return self

    def save_profile(self, name: str) -> SavedProfile:
        """Save this browser's pages, cookies and storage as a profile. The session keeps running."""
        return self._client.profiles.save(self.id, name)  # type: ignore[union-attr]

    def live_url(self) -> str:
        """A short-lived URL for watching the browser (CDP screencast)."""
        return self._client._request("POST", f"/api/v1/sessions/{self.id}/live")["url"]  # type: ignore[union-attr]

    def events(self) -> List[Event]:
        """What happened to this session: created, migrated (with the pause), forked, paused, ended and why."""
        return self._client.sessions.events(self.id)  # type: ignore[union-attr]

    def __enter__(self) -> "Session":
        return self

    def __exit__(self, *exc: Any) -> None:
        try:
            self.close()
        except Exception:
            if exc[0] is None:  # don't hide the error that ended the block
                raise


class _Sessions:
    def __init__(self, client: "Baton") -> None:
        self._c = client

    def _session(self, d: Dict[str, Any]) -> Session:
        return Session(**session_fields(d), _client=self._c)

    def create(self, profile: Optional[str] = None, *, viewport: Optional[Viewport] = None, user_agent: Optional[str] = None,
               locale: Optional[str] = None, timezone: Optional[str] = None, timeout_minutes: Optional[int] = None,
               proxy: Optional[Proxy] = None) -> Session:
        body = _create_body(profile, viewport, user_agent, locale, timezone, timeout_minutes, proxy)
        return self._session(self._c._request("POST", "/api/v1/sessions", body, wait_for_capacity=True))

    create.__doc__ = _CREATE_DOC

    def list(self) -> List[Session]:
        return [self._session(d) for d in self._c._request("GET", "/api/v1/sessions")]

    def get(self, session_id: str) -> Session:
        return self._session(self._c._request("GET", f"/api/v1/sessions/{session_id}"))

    def delete(self, session_id: str) -> None:
        self._c._request("DELETE", f"/api/v1/sessions/{session_id}")

    def migrate(self, session_id: str) -> Migration:
        return Migration.from_api(self._c._request("POST", f"/api/v1/sessions/{session_id}/migrate"))

    def pause(self, session_id: str) -> Pause:
        return Pause.from_api(self._c._request("POST", f"/api/v1/sessions/{session_id}/pause"))

    def resume(self, session_id: str) -> Session:
        return self._session(self._c._request("POST", f"/api/v1/sessions/{session_id}/resume", wait_for_capacity=True))

    def fork(self, session_id: str, count: int = 1) -> Fork:
        d = self._c._request("POST", f"/api/v1/sessions/{session_id}/fork", {"count": count})
        return Fork(d.get("parent", session_id), d.get("pausedMs", 0), [self._session(s) for s in d.get("sessions") or []], errors_of(d), d)

    def events(self, session_id: str) -> List[Event]:
        return [Event.from_api(e) for e in self._c._request("GET", f"/api/v1/sessions/{session_id}/events")]


class _Profiles:
    def __init__(self, client: "Baton") -> None:
        self._c = client

    def list(self) -> List[ProfileInfo]:
        return [ProfileInfo.from_api(p) for p in self._c._request("GET", "/api/v1/profiles")]

    def save(self, session_id: str, name: str) -> SavedProfile:
        """Save a running session's pages, cookies and storage as ``name`` (replacing one of that name)."""
        return SavedProfile.from_api(self._c._request("POST", "/api/v1/profiles", {"session": session_id, "name": name}))

    def delete(self, name: str) -> None:
        self._c._request("DELETE", f"/api/v1/profiles/{quote(name, safe='')}")


class Baton:
    """The Baton client.

    baton = Baton()  # reads BATON_API_KEY
    session = baton.sessions.create()
    """

    def __init__(self, api_key: Optional[str] = None, base_url: Optional[str] = None,
                 *, http_client: Optional[httpx.Client] = None) -> None:
        self._key, self._base = _settings(api_key, base_url)
        self._http = http_client or httpx.Client(timeout=TIMEOUT)
        self._sleep: Callable[[float], None] = time.sleep
        self._now: Callable[[], float] = time.monotonic
        self.sessions = _Sessions(self)
        self.profiles = _Profiles(self)

    def usage(self) -> Usage:
        """Sessions running and paused, profiles, and browser minutes used today, with your limits."""
        return Usage.from_api(self._request("GET", "/api/v1/usage"))

    def close(self) -> None:
        """Close the HTTP connections (sessions keep running)."""
        self._http.close()

    def __enter__(self) -> "Baton":
        return self

    def __exit__(self, *exc: Any) -> None:
        self.close()

    def _request(self, method: str, path: str, body: Any = None, *, wait_for_capacity: bool = False) -> Any:
        give_up = self._now() + CAPACITY_WAIT_S
        while True:
            res = self._http.request(method, self._base + path, json=body, headers={"Authorization": f"Bearer {self._key}"})
            wait = _retry_in(res, wait_for_capacity, give_up, self._now())
            if wait is None:
                return _result(res)
            self._sleep(wait)


# ---------------------------------------------------------------- async


@dataclass
class AsyncSession(SessionData):
    """A browser session (async). ``async with`` closes it on exit."""

    _client: Optional["AsyncBaton"] = field(default=None, repr=False, compare=False)

    async def close(self) -> None:
        await self._client.sessions.delete(self.id)  # type: ignore[union-attr]

    async def migrate(self) -> Migration:
        return await self._client.sessions.migrate(self.id)  # type: ignore[union-attr]

    async def fork(self, count: int = 1) -> Fork:
        return await self._client.sessions.fork(self.id, count)  # type: ignore[union-attr]

    async def pause(self) -> Pause:
        return await self._client.sessions.pause(self.id)  # type: ignore[union-attr]

    async def resume(self) -> "AsyncSession":
        self._update(await self._client._request("POST", f"/api/v1/sessions/{self.id}/resume", wait_for_capacity=True))  # type: ignore[union-attr]
        return self

    async def save_profile(self, name: str) -> SavedProfile:
        return await self._client.profiles.save(self.id, name)  # type: ignore[union-attr]

    async def live_url(self) -> str:
        return (await self._client._request("POST", f"/api/v1/sessions/{self.id}/live"))["url"]  # type: ignore[union-attr]

    async def events(self) -> List[Event]:
        return await self._client.sessions.events(self.id)  # type: ignore[union-attr]

    async def __aenter__(self) -> "AsyncSession":
        return self

    async def __aexit__(self, *exc: Any) -> None:
        try:
            await self.close()
        except Exception:
            if exc[0] is None:
                raise


class _AsyncSessions:
    def __init__(self, client: "AsyncBaton") -> None:
        self._c = client

    def _session(self, d: Dict[str, Any]) -> AsyncSession:
        return AsyncSession(**session_fields(d), _client=self._c)

    async def create(self, profile: Optional[str] = None, *, viewport: Optional[Viewport] = None, user_agent: Optional[str] = None,
                     locale: Optional[str] = None, timezone: Optional[str] = None, timeout_minutes: Optional[int] = None,
                     proxy: Optional[Proxy] = None) -> AsyncSession:
        body = _create_body(profile, viewport, user_agent, locale, timezone, timeout_minutes, proxy)
        return self._session(await self._c._request("POST", "/api/v1/sessions", body, wait_for_capacity=True))

    create.__doc__ = _CREATE_DOC

    async def list(self) -> List[AsyncSession]:
        return [self._session(d) for d in await self._c._request("GET", "/api/v1/sessions")]

    async def get(self, session_id: str) -> AsyncSession:
        return self._session(await self._c._request("GET", f"/api/v1/sessions/{session_id}"))

    async def delete(self, session_id: str) -> None:
        await self._c._request("DELETE", f"/api/v1/sessions/{session_id}")

    async def migrate(self, session_id: str) -> Migration:
        return Migration.from_api(await self._c._request("POST", f"/api/v1/sessions/{session_id}/migrate"))

    async def pause(self, session_id: str) -> Pause:
        return Pause.from_api(await self._c._request("POST", f"/api/v1/sessions/{session_id}/pause"))

    async def resume(self, session_id: str) -> AsyncSession:
        return self._session(await self._c._request("POST", f"/api/v1/sessions/{session_id}/resume", wait_for_capacity=True))

    async def fork(self, session_id: str, count: int = 1) -> Fork:
        d = await self._c._request("POST", f"/api/v1/sessions/{session_id}/fork", {"count": count})
        return Fork(d.get("parent", session_id), d.get("pausedMs", 0), [self._session(s) for s in d.get("sessions") or []], errors_of(d), d)

    async def events(self, session_id: str) -> List[Event]:
        return [Event.from_api(e) for e in await self._c._request("GET", f"/api/v1/sessions/{session_id}/events")]


class _AsyncProfiles:
    def __init__(self, client: "AsyncBaton") -> None:
        self._c = client

    async def list(self) -> List[ProfileInfo]:
        return [ProfileInfo.from_api(p) for p in await self._c._request("GET", "/api/v1/profiles")]

    async def save(self, session_id: str, name: str) -> SavedProfile:
        return SavedProfile.from_api(await self._c._request("POST", "/api/v1/profiles", {"session": session_id, "name": name}))

    async def delete(self, name: str) -> None:
        await self._c._request("DELETE", f"/api/v1/profiles/{quote(name, safe='')}")


class AsyncBaton:
    """The Baton client for asyncio (browser-use, Playwright's async API).

    async with AsyncBaton() as baton:
        session = await baton.sessions.create()
    """

    def __init__(self, api_key: Optional[str] = None, base_url: Optional[str] = None,
                 *, http_client: Optional[httpx.AsyncClient] = None) -> None:
        self._key, self._base = _settings(api_key, base_url)
        self._http = http_client or httpx.AsyncClient(timeout=TIMEOUT)
        self._sleep: Callable[[float], Any] = asyncio.sleep
        self._now: Callable[[], float] = time.monotonic
        self.sessions = _AsyncSessions(self)
        self.profiles = _AsyncProfiles(self)

    async def usage(self) -> Usage:
        return Usage.from_api(await self._request("GET", "/api/v1/usage"))

    async def close(self) -> None:
        await self._http.aclose()

    async def __aenter__(self) -> "AsyncBaton":
        return self

    async def __aexit__(self, *exc: Any) -> None:
        await self.close()

    async def _request(self, method: str, path: str, body: Any = None, *, wait_for_capacity: bool = False) -> Any:
        give_up = self._now() + CAPACITY_WAIT_S
        while True:
            res = await self._http.request(method, self._base + path, json=body, headers={"Authorization": f"Bearer {self._key}"})
            wait = _retry_in(res, wait_for_capacity, give_up, self._now())
            if wait is None:
                return _result(res)
            await self._sleep(wait)
