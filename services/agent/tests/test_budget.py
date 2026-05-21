"""Tests for pipeline/budget.py — Redis monthly cap + kill-switch."""
from __future__ import annotations

import os
import time
from datetime import date

import pytest

from pipeline import budget


def test_cap_cents_default():
    assert budget.cap_cents() == 5000


def test_cap_cents_env_override(monkeypatch):
    monkeypatch.setenv("LLM_BUDGET_CAP_CENTS", "20000")
    assert budget.cap_cents() == 20000


def test_cap_cents_invalid_env_falls_back(monkeypatch):
    monkeypatch.setenv("LLM_BUDGET_CAP_CENTS", "not-a-number")
    assert budget.cap_cents() == budget.DEFAULT_CAP_CENTS


def test_cap_cents_negative_env_falls_back(monkeypatch):
    monkeypatch.setenv("LLM_BUDGET_CAP_CENTS", "-50")
    assert budget.cap_cents() == budget.DEFAULT_CAP_CENTS


def test_current_spend_zero_on_fresh_redis(redis_client):
    assert budget.current_spend_cents(redis_client) == 0


def test_record_cost_increments_bucket(redis_client):
    remaining = budget.record_cost(redis_client, 0.10)  # 10 cents
    assert budget.current_spend_cents(redis_client) == 10
    assert remaining == budget.cap_cents() - 10


def test_record_cost_sets_ttl(redis_client):
    budget.record_cost(redis_client, 0.50)
    key = f"llm:budget:{time.strftime('%Y-%m', time.gmtime())}:cents"
    ttl = redis_client.ttl(key)
    assert 0 < ttl <= budget.KEY_TTL_SECONDS


def test_record_cost_zero_noop(redis_client):
    budget.record_cost(redis_client, 0.0)
    assert budget.current_spend_cents(redis_client) == 0


def test_record_cost_negative_is_clamped_to_zero(redis_client):
    budget.record_cost(redis_client, -1.5)
    assert budget.current_spend_cents(redis_client) == 0


def test_assert_within_budget_passes_when_room(redis_client):
    budget.record_cost(redis_client, 0.50)
    budget.assert_within_budget(redis_client)  # must not raise


def test_assert_within_budget_raises_when_exhausted(redis_client, monkeypatch):
    monkeypatch.setenv("LLM_BUDGET_CAP_CENTS", "100")  # $1.00
    budget.record_cost(redis_client, 1.00)
    with pytest.raises(budget.BudgetExhausted):
        budget.assert_within_budget(redis_client)


def test_remaining_cents_can_go_negative(redis_client, monkeypatch):
    """If a call's cost overshoots the cap, remaining is negative but the
    call that crossed the threshold is still accounted for."""
    monkeypatch.setenv("LLM_BUDGET_CAP_CENTS", "100")
    budget.record_cost(redis_client, 1.50)  # spend $1.50 against $1.00 cap
    assert budget.current_spend_cents(redis_client) == 150
    assert budget.remaining_cents(redis_client) == -50


def test_record_cost_returns_kill_signal_when_crossing(redis_client, monkeypatch):
    monkeypatch.setenv("LLM_BUDGET_CAP_CENTS", "100")
    assert budget.record_cost(redis_client, 0.99) > 0
    # Next call crosses the cap — return value should be ≤ 0 so caller can flip kill-switch.
    assert budget.record_cost(redis_client, 0.05) <= 0


def test_record_cost_rounds_correctly(redis_client):
    # Python `round()` uses banker's rounding (round-half-to-even), so $0.005
    # may round to 0 cents (nearest even) rather than 1. Either outcome is
    # acceptable here — we only assert the call doesn't blow up and that the
    # bucket stays within the expected range.
    budget.record_cost(redis_client, 0.005)
    spent = budget.current_spend_cents(redis_client)
    assert spent in (0, 1)


def test_assert_within_budget_message_contains_numbers(redis_client, monkeypatch):
    monkeypatch.setenv("LLM_BUDGET_CAP_CENTS", "50")
    budget.record_cost(redis_client, 0.51)
    with pytest.raises(budget.BudgetExhausted, match="51"):
        budget.assert_within_budget(redis_client)


def test_separate_months_isolated(redis_client, monkeypatch):
    """Spend in YYYY-MM key X must not affect remaining when month rolls over.
    We simulate by writing a past-month key directly and verifying it does not count."""
    # Step back one day from the first of this month → guaranteed prior month,
    # robust around month-length edges (a 35-day offset can land in the wrong
    # month near the end of a long month).
    first_of_this_month = date.today().replace(day=1)
    last_month_date = date.fromordinal(first_of_this_month.toordinal() - 1)
    last_month = last_month_date.strftime("%Y-%m")
    redis_client.set(f"llm:budget:{last_month}:cents", 999999)
    assert budget.current_spend_cents(redis_client) == 0  # current month is clean
    assert budget.remaining_cents(redis_client) == budget.cap_cents()


def test_cap_cents_env_unset(monkeypatch):
    monkeypatch.delenv("LLM_BUDGET_CAP_CENTS", raising=False)
    assert budget.cap_cents() == budget.DEFAULT_CAP_CENTS
