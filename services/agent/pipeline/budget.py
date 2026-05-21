"""Monthly LLM spend tracker + kill-switch.

Stores cumulative spend in Redis as cents (integers) keyed by year-month.
Resets implicitly when the calendar month rolls over — key TTL is 35 days so
old buckets expire automatically.

Hard cap default: 5000 cents = $50.00/month, owner-set 2026-05-21.
Override via env var ``LLM_BUDGET_CAP_CENTS`` for staging / experiments.
"""
from __future__ import annotations

import logging
import os
import time

import redis as redis_lib

logger = logging.getLogger("agent.pipeline.budget")

# Default global monthly cap in CENTS ($50.00 = 5000 cents).
DEFAULT_CAP_CENTS = 5000

# Key TTL: 35 days so the previous month's key expires before we'd ever reuse it.
KEY_TTL_SECONDS = 35 * 24 * 3600


class BudgetExhausted(RuntimeError):
    """Raised when the monthly cap has been reached. Callers must not invoke LLM APIs."""


def cap_cents() -> int:
    """Resolve the monthly cap in cents. Env var overrides default."""
    raw = os.getenv("LLM_BUDGET_CAP_CENTS")
    if raw is None:
        return DEFAULT_CAP_CENTS
    try:
        v = int(raw)
        return v if v >= 0 else DEFAULT_CAP_CENTS
    except ValueError:
        logger.warning("LLM_BUDGET_CAP_CENTS=%r is not an int; using default %d", raw, DEFAULT_CAP_CENTS)
        return DEFAULT_CAP_CENTS


def _current_key() -> str:
    return f"llm:budget:{time.strftime('%Y-%m', time.gmtime())}:cents"


def current_spend_cents(rdb: redis_lib.Redis) -> int:
    """Cumulative spend this calendar month, in cents. 0 if no calls yet."""
    raw = rdb.get(_current_key())
    if raw is None:
        return 0
    try:
        return int(raw)
    except (TypeError, ValueError):
        return 0


def remaining_cents(rdb: redis_lib.Redis) -> int:
    """Cents still available before the cap trips. May be negative if over."""
    return cap_cents() - current_spend_cents(rdb)


def assert_within_budget(rdb: redis_lib.Redis) -> None:
    """Raise BudgetExhausted if no more spend is allowed this month."""
    if remaining_cents(rdb) <= 0:
        raise BudgetExhausted(
            f"LLM monthly budget exhausted "
            f"({current_spend_cents(rdb)} / {cap_cents()} cents)"
        )


def record_cost(rdb: redis_lib.Redis, cost_usd: float) -> int:
    """
    Add ``cost_usd`` to this month's bucket.

    Returns the updated remaining cents (may be ≤0 after this call). Always
    refreshes the key TTL so a long quiet period cannot leave a stale value.
    Caller is responsible for raising BudgetExhausted on subsequent calls;
    this function never raises, so the cost from the call that *crossed* the
    cap is still accounted for.
    """
    cost_cents = max(0, round(cost_usd * 100))
    if cost_cents == 0:
        return remaining_cents(rdb)

    pipe = rdb.pipeline(transaction=True)
    pipe.incrby(_current_key(), cost_cents)
    pipe.expire(_current_key(), KEY_TTL_SECONDS)
    new_total, _ = pipe.execute()

    remaining = cap_cents() - int(new_total)
    if remaining <= 0:
        logger.error(
            "LLM monthly budget exhausted: spend=%d cents, cap=%d cents",
            new_total, cap_cents(),
        )
    return remaining
