"""Redis short-term memory: recent topics, angles, sources per market (3-14 day TTL)."""
import json
import time

import redis as redis_lib

MEMORY_TTL = 7 * 24 * 3600  # 7 days


def fetch(rdb: redis_lib.Redis, market: str) -> dict:
    key = f"memory:{market}"
    raw = rdb.hgetall(key)
    if not raw:
        return {}
    return {k.decode(): json.loads(v) if v.startswith(b"{") else v.decode() for k, v in raw.items()}


def write(rdb: redis_lib.Redis, market: str, topic_name: str, content_summary: str) -> None:
    key = f"memory:{market}"
    entry = json.dumps({
        "topic_name": topic_name,
        "summary": content_summary[:200],
        "updated_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    })
    pipe = rdb.pipeline()
    pipe.hset(key, topic_name, entry)
    pipe.expire(key, MEMORY_TTL)
    pipe.execute()
