"""Redis fast-path corrections: editor overrides with 48h TTL."""
import json

import redis as redis_lib


def fetch(rdb: redis_lib.Redis, market: str) -> dict:
    """Return all active correction entries for a market (48h TTL keys)."""
    pattern = f"corrections:{market}:*"
    corrections = {}
    for key in rdb.scan_iter(pattern, count=50):
        raw = rdb.get(key)
        if raw:
            try:
                payload = json.loads(raw)
                cid = key.decode().rsplit(":", 1)[-1]
                corrections[cid] = payload
            except (json.JSONDecodeError, AttributeError):
                pass
    return corrections
