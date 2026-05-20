"""Redis token bucket rate limiter — prevents LLM budget explosions per market."""
import logging
import time

import redis as redis_lib

logger = logging.getLogger("agent.pipeline.rate_limit")

# Bucket config: 10 tokens, refill 1/min
BUCKET_CAPACITY = 10
REFILL_RATE = 1  # tokens per minute
REFILL_INTERVAL = 60  # seconds


def acquire(rdb: redis_lib.Redis, market: str) -> bool:
    """
    Try to consume one token from market's LLM bucket.
    Returns True if token acquired, False if rate limit exceeded.
    """
    bucket_key = f"llm:rate:{market}"
    refill_key = f"llm:rate:{market}:last_refill"

    try:
        pipe = rdb.pipeline(transaction=True)
        pipe.watch(bucket_key, refill_key)
        pipe.multi()

        now = time.time()
        tokens_raw = rdb.get(bucket_key)
        last_refill_raw = rdb.get(refill_key)

        tokens = int(tokens_raw) if tokens_raw else BUCKET_CAPACITY
        last_refill = float(last_refill_raw) if last_refill_raw else now

        # Refill based on elapsed time
        elapsed_minutes = (now - last_refill) / REFILL_INTERVAL
        refill = int(elapsed_minutes * REFILL_RATE)
        tokens = min(BUCKET_CAPACITY, tokens + refill)

        if tokens <= 0:
            pipe.reset()
            logger.warning("rate limit exceeded for market=%s", market)
            return False

        pipe.set(bucket_key, tokens - 1, ex=3600)
        pipe.set(refill_key, now, ex=3600)
        pipe.execute()
        return True
    except redis_lib.WatchError:
        return acquire(rdb, market)  # retry on concurrent modification
    except Exception as e:
        logger.warning("rate limit check failed: %s — allowing", e)
        return True
