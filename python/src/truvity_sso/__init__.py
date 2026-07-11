"""Python client for the SSO administration API.

Every operation is scoped to one product, authenticated with the product's
machine key. Tokens are minted (JWT profile) and refreshed automatically.

    client = Client(
        endpoint="https://sso-api.example.com",
        product="acme",
        issuer="https://login.example.com",
        project_id="1234567890",
        key=key_json,  # the machine key JSON issued to your product
    )
    user = client.create_user(email="jane@example.com")
"""

from __future__ import annotations

import json
import threading
import time
import uuid
from dataclasses import dataclass, field
from typing import Any
from urllib.parse import quote

import jwt as pyjwt
import requests

__all__ = ["ApiError", "Client"]


class ApiError(Exception):
    """A non-2xx response (RFC 9457 problem shape)."""

    def __init__(self, status: int, title: str, detail: str = "", errors: list[dict[str, Any]] | None = None):
        super().__init__(f"sso: {status} {title}: {detail}")
        self.status = status
        self.title = title
        self.detail = detail
        self.errors = errors or []

    def existing_central_id(self) -> str:
        """The already-existing identity id from a create-user conflict —
        use it with onboard() instead. Empty when absent."""
        if self.status != 409:
            return ""
        for d in self.errors:
            value = d.get("value")
            if isinstance(value, str) and value:
                return value
        return ""


@dataclass
class _Token:
    value: str
    expires_at: float


class Client:
    """Product-scoped handle on the administration API. Thread-safe."""

    def __init__(
        self,
        *,
        endpoint: str,
        product: str,
        issuer: str,
        project_id: str,
        key: str | dict[str, str],
        session: requests.Session | None = None,
    ):
        for name, value in {
            "endpoint": endpoint, "product": product, "issuer": issuer,
            "project_id": project_id, "key": key,
        }.items():
            if not value:
                raise ValueError(f"sso: {name} is required")
        self._endpoint = endpoint.rstrip("/")
        self._product = product
        self._issuer = issuer.rstrip("/")
        self._scope = f"openid urn:zitadel:iam:org:project:id:{project_id}:aud"
        self._key = json.loads(key) if isinstance(key, str) else key
        self._http = session or requests.Session()
        self._token: _Token | None = None
        self._lock = threading.Lock()

    # -- users ------------------------------------------------------------

    def create_user(
        self, *, email: str, first_name: str = "", last_name: str = "",
        invite_mode: str = "", idempotency_key: str = "",
    ) -> dict[str, Any]:
        """Create the central identity and send the invitation (409 → onboard)."""
        body: dict[str, Any] = {"email": email}
        if first_name:
            body["firstName"] = first_name
        if last_name:
            body["lastName"] = last_name
        if invite_mode:
            body["inviteMode"] = invite_mode
        return self._do("POST", "/users", body=body, mutation=True, idempotency_key=idempotency_key)

    def onboard(
        self, *, central_id: str = "", email: str = "", idempotency_key: str = "",
    ) -> dict[str, Any]:
        """Mark an EXISTING identity (by central_id, or by VERIFIED email) as
        a member of the calling product. Idempotent."""
        body: dict[str, Any] = {}
        if central_id:
            body["centralId"] = central_id
        if email:
            body["email"] = email
        return self._do("POST", "/users/onboard", body=body, mutation=True, idempotency_key=idempotency_key)

    def list_users(self, *, cursor: str = "", limit: int = 0, email_prefix: str = "") -> dict[str, Any]:
        """One page of the product's onboarded users."""
        params = {}
        if cursor:
            params["cursor"] = cursor
        if limit:
            params["limit"] = str(limit)
        if email_prefix:
            params["email"] = email_prefix
        return self._do("GET", "/users", params=params)

    def get_user(self, central_id: str) -> dict[str, Any]:
        """One user's detail within the product scope (404 outside it)."""
        return self._do("GET", f"/users/{quote(central_id, safe='')}")

    # -- support operations ------------------------------------------------

    def send_verification_email(self, central_id: str) -> dict[str, Any]:
        return self._action(central_id, "/verification-email")

    def reset_mfa(self, central_id: str) -> dict[str, Any]:
        """Remove the user's second factors (GLOBAL effect)."""
        return self._action(central_id, "/mfa/reset")

    def reset_password(self, central_id: str) -> dict[str, Any]:
        """Start a central password reset (GLOBAL effect)."""
        return self._action(central_id, "/password/reset")

    def lock(self, central_id: str) -> dict[str, Any]:
        """Deactivate the central identity (GLOBAL effect)."""
        return self._action(central_id, "/lock")

    def unlock(self, central_id: str) -> dict[str, Any]:
        """Reactivate a locked identity (GLOBAL effect)."""
        return self._action(central_id, "/unlock")

    # -- sessions -----------------------------------------------------------

    def list_sessions(self, central_id: str) -> dict[str, Any]:
        return self._do("GET", f"/users/{quote(central_id, safe='')}/sessions")

    def terminate_session(self, central_id: str, session_id: str) -> None:
        self._do(
            "DELETE",
            f"/users/{quote(central_id, safe='')}/sessions/{quote(session_id, safe='')}",
            mutation=True,
        )

    def terminate_all_sessions(self, central_id: str) -> None:
        self._do("DELETE", f"/users/{quote(central_id, safe='')}/sessions", mutation=True)

    # -- audit ---------------------------------------------------------------

    def get_audit(self, central_id: str, *, cursor: str = "", limit: int = 0) -> dict[str, Any]:
        """The user's audit timeline within the product scope."""
        params = {}
        if cursor:
            params["cursor"] = cursor
        if limit:
            params["limit"] = str(limit)
        return self._do("GET", f"/users/{quote(central_id, safe='')}/audit", params=params)

    # -- plumbing --------------------------------------------------------------

    def _action(self, central_id: str, suffix: str) -> dict[str, Any]:
        return self._do("POST", f"/users/{quote(central_id, safe='')}{suffix}", mutation=True)

    def _access_token(self) -> str:
        with self._lock:
            if self._token and time.time() < self._token.expires_at - 120:
                return self._token.value
            now = int(time.time())
            assertion = pyjwt.encode(
                {
                    "iss": self._key["userId"],
                    "sub": self._key["userId"],
                    "aud": self._issuer,
                    "iat": now,
                    "exp": now + 600,
                },
                self._key["key"],
                algorithm="RS256",
                headers={"kid": self._key["keyId"]},
            )
            resp = self._http.post(
                f"{self._issuer}/oauth/v2/token",
                data={
                    "grant_type": "urn:ietf:params:oauth:grant-type:jwt-bearer",
                    "scope": self._scope,
                    "assertion": assertion,
                },
                timeout=30,
            )
            if resp.status_code >= 300:
                raise ApiError(resp.status_code, "TokenError", resp.text)
            data = resp.json()
            self._token = _Token(data["access_token"], time.time() + data["expires_in"])
            return self._token.value

    def _do(
        self, method: str, path: str, *, body: dict[str, Any] | None = None,
        params: dict[str, str] | None = None, mutation: bool = False, idempotency_key: str = "",
    ) -> Any:
        headers = {"Authorization": f"Bearer {self._access_token()}"}
        if mutation:
            headers["Idempotency-Key"] = idempotency_key or str(uuid.uuid4())
        resp = self._http.request(
            method,
            f"{self._endpoint}/v1/products/{quote(self._product, safe='')}{path}",
            json=body,
            params=params or None,
            headers=headers,
            timeout=30,
        )
        if resp.status_code >= 300:
            try:
                problem = resp.json()
            except ValueError:
                problem = {}
            raise ApiError(
                resp.status_code,
                problem.get("title", resp.reason),
                problem.get("detail", ""),
                problem.get("errors"),
            )
        return resp.json() if resp.content else None
