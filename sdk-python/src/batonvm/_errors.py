"""Errors from the Baton API. Each carries the HTTP status; the common ones
have their own class so callers can catch exactly what they handle."""

from __future__ import annotations


class BatonError(Exception):
    """An error from the Baton API, with its HTTP status."""

    def __init__(self, message: str, status: int) -> None:
        super().__init__(message)
        self.message = message
        self.status = status

    def __repr__(self) -> str:
        return f"{type(self).__name__}({self.message!r}, status={self.status})"


class AuthError(BatonError):
    """401: no API key, or a wrong or revoked one."""


class NotFoundError(BatonError):
    """404: no such session or profile (or not yours)."""


class LimitError(BatonError):
    """429: over a limit of your account (sessions at once, per minute, per day...)."""


class CapacityError(BatonError):
    """503: every machine is full. Creating and resuming wait for a new one first."""


_BY_STATUS = {401: AuthError, 404: NotFoundError, 429: LimitError, 503: CapacityError}


def error_for(status: int, message: str) -> BatonError:
    return _BY_STATUS.get(status, BatonError)(message, status)
