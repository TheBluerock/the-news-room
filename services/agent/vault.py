"""Vault KV v2 secret loader — reads from HTTP API (dev) or sidecar filesystem (K8s)."""

import os
import json
import urllib.request
from pathlib import Path


def load(service: str) -> dict[str, str]:
    """Load secrets for the given service from Vault.

    In K8s, the Vault Agent sidecar writes secrets to /vault/secrets/<service>.json.
    In local dev, secrets are read from the Vault HTTP API.
    """
    sidecar_path = Path(f"/vault/secrets/{service}.json")
    if sidecar_path.exists():
        return json.loads(sidecar_path.read_text())

    addr = os.getenv("VAULT_ADDR", "http://localhost:8200")
    token = os.getenv("VAULT_TOKEN", "")
    url = f"{addr}/v1/secret/data/newsroom/{service}"

    req = urllib.request.Request(url, headers={"X-Vault-Token": token})
    with urllib.request.urlopen(req) as resp:
        body = json.loads(resp.read())
    return body["data"]["data"]


def require(secrets: dict[str, str], key: str) -> str:
    value = secrets.get(key, "")
    if not value:
        raise RuntimeError(f"vault: missing required secret '{key}'")
    return value
