import os

# Who may use this service. Everything here runs on a GPU somebody pays for and
# writes into a store everyone shares, so the guest list is the whole defence:
# the endpoint itself is reachable by anyone who knows the URL.
ALLOWED_DOMAINS = ("nexaedge.com", "ppfxlabs.ai")

_GOOGLE_ISSUERS = ("accounts.google.com", "https://accounts.google.com")


class Unauthorized(Exception):
    """The caller is not someone this service serves."""


class Identity:
    """Who Google says is calling."""

    def __init__(self, email: str, domain: str):
        self.email = email
        self.domain = domain

    def __repr__(self) -> str:
        return f"Identity({self.email!r})"


def identify(authorization: str | None) -> Identity:
    """Verify a Google ID token and return who it belongs to.

    Raises Unauthorized for anything short of a signed, unexpired token issued
    to this service for somebody at an allowed domain.
    """
    if not authorization:
        raise Unauthorized("no credentials: sign in with 'plaud auth login'")

    scheme, _, token = authorization.partition(" ")
    if scheme.lower() != "bearer" or not token.strip():
        raise Unauthorized("expected an 'Authorization: Bearer <token>' header")

    client_id = os.environ.get("GOOGLE_CLIENT_ID", "")
    if not client_id:
        # Verifying without an audience would accept a token minted for any
        # application at all, so refuse rather than degrade.
        raise Unauthorized("this service is missing its Google client configuration")

    claims = _verified_claims(token.strip(), client_id)

    if claims.get("iss") not in _GOOGLE_ISSUERS:
        raise Unauthorized("that token was not issued by Google")
    if not claims.get("email_verified"):
        raise Unauthorized("that Google account has no verified e-mail")

    email = str(claims.get("email", "")).lower()
    domain = email.rpartition("@")[2]
    if domain not in ALLOWED_DOMAINS:
        raise Unauthorized(
            f"{email or 'that account'} is outside the domains this service serves "
            f"({', '.join(ALLOWED_DOMAINS)})"
        )

    return Identity(email=email, domain=domain)


def _verified_claims(token: str, client_id: str) -> dict:
    """Check the signature, the audience and the expiry, in Google's own library."""
    from google.auth.transport import requests as google_requests
    from google.oauth2 import id_token as google_id_token

    try:
        return google_id_token.verify_oauth2_token(
            token, google_requests.Request(), client_id
        )
    except ValueError as err:
        raise Unauthorized(f"that sign-in is not valid: {err}") from err
