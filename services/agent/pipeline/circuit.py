"""
Per-market LLM circuit breaker.

States:  CLOSED → OPEN (after ≥5 failures in 60s) → HALF_OPEN (after 30s) → CLOSED
Primary: OpenAI GPT-4o
Fallback: Anthropic claude-sonnet-4-6

Prometheus metric: llm_circuit_state{market, provider} = 0 (closed) | 1 (open) | 2 (half-open)
"""
import logging
import time

import redis as redis_lib
from prometheus_client import Gauge

logger = logging.getLogger("agent.pipeline.circuit")

FAILURE_THRESHOLD = 5
FAILURE_WINDOW = 60   # seconds
HALF_OPEN_AFTER = 30  # seconds

STATE_CLOSED = 0
STATE_OPEN = 1
STATE_HALF_OPEN = 2

circuit_state_gauge = Gauge(
    "llm_circuit_state",
    "LLM circuit breaker state (0=closed, 1=open, 2=half-open)",
    ["market", "provider"],
)

PROVIDERS = ("openai", "anthropic")


def get_state(rdb: redis_lib.Redis, market: str, provider: str) -> int:
    state_key = f"circuit:{market}:{provider}:state"
    open_at_key = f"circuit:{market}:{provider}:opened_at"

    raw = rdb.get(state_key)
    if raw is None:
        return STATE_CLOSED

    state = int(raw)
    if state == STATE_OPEN:
        opened_at = rdb.get(open_at_key)
        if opened_at and (time.time() - float(opened_at)) >= HALF_OPEN_AFTER:
            rdb.set(state_key, STATE_HALF_OPEN)
            state = STATE_HALF_OPEN
    return state


def record_success(rdb: redis_lib.Redis, market: str, provider: str) -> None:
    state_key = f"circuit:{market}:{provider}:state"
    fail_key = f"circuit:{market}:{provider}:failures"
    rdb.delete(state_key, fail_key)
    circuit_state_gauge.labels(market=market, provider=provider).set(STATE_CLOSED)


def record_failure(rdb: redis_lib.Redis, market: str, provider: str) -> None:
    fail_key = f"circuit:{market}:{provider}:failures"
    count = rdb.incr(fail_key)
    rdb.expire(fail_key, FAILURE_WINDOW)

    if count >= FAILURE_THRESHOLD:
        state_key = f"circuit:{market}:{provider}:state"
        open_at_key = f"circuit:{market}:{provider}:opened_at"
        rdb.set(state_key, STATE_OPEN)
        rdb.set(open_at_key, time.time())
        circuit_state_gauge.labels(market=market, provider=provider).set(STATE_OPEN)
        logger.warning("circuit OPENED market=%s provider=%s (failures=%d)", market, provider, count)


def is_available(rdb: redis_lib.Redis, market: str, provider: str) -> bool:
    state = get_state(rdb, market, provider)
    circuit_state_gauge.labels(market=market, provider=provider).set(state)
    return state in (STATE_CLOSED, STATE_HALF_OPEN)
