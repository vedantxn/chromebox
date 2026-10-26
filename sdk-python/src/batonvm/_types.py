"""What the API returns, as dataclasses. Each keeps the raw response in `raw`,
so fields a newer API adds are there to read, and never break an older SDK."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, Dict, List, Optional


def _f(d: Dict[str, Any], key: str, default: Any = None) -> Any:
    v = d.get(key, default)
    return default if v is None else v


@dataclass
class SessionData:
    id: str
    status: str
    node: Optional[str] = None
    created_at: Optional[str] = None
    expires_at: Optional[str] = None
    ended_at: Optional[str] = None
    end_reason: Optional[str] = None
    migrations: int = 0
    #: wss URL for Playwright's ``chromium.connect_over_cdp`` or ``pyppeteer``.
    #: Only returned when a session is created, resumed or forked.
    connect_url: Optional[str] = None
    #: The options the session was created with, as the API reports them (camelCase;
    #: a proxy's password shows as "********").
    options: Dict[str, Any] = field(default_factory=dict)
    raw: Dict[str, Any] = field(default_factory=dict, repr=False, compare=False)

    def _update(self, d: Dict[str, Any]) -> None:
        self.id = _f(d, "id", self.id)
        self.status = _f(d, "status", self.status)
        self.node = _f(d, "node", self.node)
        self.created_at = _f(d, "createdAt", self.created_at)
        self.expires_at = _f(d, "expiresAt", self.expires_at)
        self.ended_at = _f(d, "endedAt", self.ended_at)
        self.end_reason = _f(d, "endReason", self.end_reason)
        self.migrations = _f(d, "migrations", self.migrations)
        self.connect_url = _f(d, "connectUrl", self.connect_url)
        self.options = _f(d, "options", self.options)
        self.raw = d


@dataclass
class Migration:
    """A move to another machine. Your connections stay open through it."""

    from_node: str
    to_node: str
    #: How long the browser was paused while it moved.
    paused_ms: int
    #: How long it was unreachable (the pause plus the network switch).
    unreachable_ms: int
    raw: Dict[str, Any] = field(default_factory=dict, repr=False, compare=False)

    @classmethod
    def from_api(cls, d: Dict[str, Any]) -> "Migration":
        return cls(_f(d, "from", ""), _f(d, "to", ""), _f(d, "pausedMs", 0), _f(d, "unreachableMs", 0), d)


@dataclass
class Pause:
    """A browser stored and stopped; it costs nothing until it is resumed."""

    #: How long the browser was paused while its snapshot was taken.
    paused_ms: int
    #: The pause request to the browser stored and stopped.
    total_ms: int
    #: What was stored, in MiB (memory without zero pages, plus disk changes).
    stored_mib: float
    raw: Dict[str, Any] = field(default_factory=dict, repr=False, compare=False)

    @classmethod
    def from_api(cls, d: Dict[str, Any]) -> "Pause":
        return cls(_f(d, "pausedMs", 0), _f(d, "totalMs", 0), _f(d, "storedMiB", 0.0), d)


@dataclass
class ProfileInfo:
    name: str
    created_at: Optional[str] = None
    stored_mib: float = 0.0
    raw: Dict[str, Any] = field(default_factory=dict, repr=False, compare=False)

    @classmethod
    def from_api(cls, d: Dict[str, Any]) -> "ProfileInfo":
        return cls(_f(d, "name", ""), _f(d, "createdAt"), _f(d, "storedMiB", 0.0), d)


@dataclass
class SavedProfile:
    name: str
    #: How long the browser was paused while its state was taken (it keeps running after).
    paused_ms: int
    upload_ms: int
    stored_mib: float
    raw: Dict[str, Any] = field(default_factory=dict, repr=False, compare=False)

    @classmethod
    def from_api(cls, d: Dict[str, Any]) -> "SavedProfile":
        return cls(_f(d, "name", ""), _f(d, "pausedMs", 0), _f(d, "uploadMs", 0), _f(d, "storedMiB", 0.0), d)


@dataclass
class Event:
    """One thing that happened to a session: created, migrated, forked, paused, ended..."""

    at: str
    kind: str
    detail: Dict[str, Any] = field(default_factory=dict)

    @classmethod
    def from_api(cls, d: Dict[str, Any]) -> "Event":
        return cls(_f(d, "at", ""), _f(d, "kind", ""), _f(d, "detail", {}))


@dataclass
class Usage:
    running: int
    max_running: int
    paused: int
    max_paused: int
    profiles: int
    max_profiles: int
    minutes_today: float
    max_minutes_per_day: float
    session_lifetime_minutes: float
    #: The longest ``timeout_minutes`` a session may ask for.
    max_session_minutes: float = 0
    raw: Dict[str, Any] = field(default_factory=dict, repr=False, compare=False)

    @classmethod
    def from_api(cls, d: Dict[str, Any]) -> "Usage":
        keys = ["running", "max_running", "paused", "max_paused", "profiles", "max_profiles",
                "minutes_today", "max_minutes_per_day", "session_lifetime_minutes", "max_session_minutes"]
        return cls(*[_f(d, k, 0) for k in keys], raw=d)  # type: ignore[arg-type]


def session_fields(d: Dict[str, Any]) -> Dict[str, Any]:
    """The SessionData constructor arguments for an API session."""
    return dict(
        id=_f(d, "id", ""), status=_f(d, "status", ""), node=d.get("node"), created_at=d.get("createdAt"),
        expires_at=d.get("expiresAt"), ended_at=d.get("endedAt"), end_reason=d.get("endReason"),
        migrations=_f(d, "migrations", 0), connect_url=d.get("connectUrl"), options=_f(d, "options", {}), raw=d,
    )


def errors_of(d: Dict[str, Any]) -> List[str]:
    return list(_f(d, "errors", []))
