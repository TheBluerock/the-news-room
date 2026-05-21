"""Cost computation + budget kill-switch integration for pipeline/llm.py."""
from __future__ import annotations

import json
from types import SimpleNamespace
from unittest.mock import MagicMock

import pytest

from pipeline import budget, circuit, llm


VALID_PAYLOAD = {
    "title": "Barolo 2018",
    "excerpt": "lead",
    "section": "cantine",
    "author": "Maria Rossi",
    "tags": ["barolo", "piedmont", "docg", "2018", "tasting"],
    "body": "Body. " * 20,
}


def _openai_response_with_usage(payload: dict, pt: int, ct: int):
    return SimpleNamespace(
        choices=[SimpleNamespace(message=SimpleNamespace(content=json.dumps(payload)))],
        usage=SimpleNamespace(prompt_tokens=pt, completion_tokens=ct),
    )


def _anthropic_response_with_usage(payload: dict, pt: int, ct: int):
    return SimpleNamespace(
        content=[SimpleNamespace(text=json.dumps(payload))],
        usage=SimpleNamespace(input_tokens=pt, output_tokens=ct),
    )


def _openai_client(payload, pt, ct):
    c = MagicMock()
    c.chat.completions.create.return_value = _openai_response_with_usage(payload, pt, ct)
    return c


def _anthropic_client(payload, pt, ct):
    c = MagicMock()
    c.messages.create.return_value = _anthropic_response_with_usage(payload, pt, ct)
    return c


# ── cost_usd pure ─────────────────────────────────────────────────────────────

def test_cost_usd_gpt4o_known_quantities():
    # gpt-4o: $2.50/1M input, $10.00/1M output
    # 1000 input + 500 output = (1000/1M * 2.50) + (500/1M * 10.00) = 0.0025 + 0.005 = 0.0075
    cost = llm.cost_usd("gpt-4o", 1000, 500)
    assert abs(cost - 0.0075) < 1e-9


def test_cost_usd_claude_known_quantities():
    # claude-sonnet-4-6: $3.00/1M input, $15.00/1M output
    # 1000 input + 500 output = (1000/1M * 3.00) + (500/1M * 15.00)
    #                         = 0.003 + 0.0075 = 0.0105
    cost = llm.cost_usd("claude-sonnet-4-6", 1000, 500)
    assert abs(cost - (0.003 + 0.0075)) < 1e-9


def test_cost_usd_unknown_model_zero():
    assert llm.cost_usd("nonexistent-model", 1000, 500) == 0.0


def test_cost_usd_zero_tokens():
    assert llm.cost_usd("gpt-4o", 0, 0) == 0.0


# ── generate() captures usage ────────────────────────────────────────────────

def test_generate_openai_returns_usage(redis_client):
    oc = _openai_client(VALID_PAYLOAD, 1234, 567)
    ac = MagicMock()
    structured, article_id, usage = llm.generate(redis_client, "italy", "PROMPT", oc, ac)
    assert structured["title"] == "Barolo 2018"
    assert article_id
    assert usage["prompt_tokens"] == 1234
    assert usage["completion_tokens"] == 567
    assert usage["model"] == "gpt-4o"
    assert usage["cost_usd"] > 0


def test_generate_anthropic_returns_usage(redis_client):
    # Trip OpenAI circuit so we fall through to Anthropic.
    for _ in range(circuit.FAILURE_THRESHOLD):
        circuit.record_failure(redis_client, "italy", "openai")
    ac = _anthropic_client(VALID_PAYLOAD, 800, 400)
    structured, _, usage = llm.generate(redis_client, "italy", "PROMPT", MagicMock(), ac)
    assert usage["model"] == "claude-sonnet-4-6"
    assert usage["prompt_tokens"] == 800
    assert usage["completion_tokens"] == 400


def test_generate_missing_usage_attr_defaults_zero(redis_client):
    """Some mock responses won't have a `usage` attr — caller must not crash."""
    c = MagicMock()
    # Return a response without `usage` field at all.
    c.chat.completions.create.return_value = SimpleNamespace(
        choices=[SimpleNamespace(message=SimpleNamespace(content=json.dumps(VALID_PAYLOAD)))],
    )
    _, _, usage = llm.generate(redis_client, "italy", "PROMPT", c, MagicMock())
    assert usage["prompt_tokens"] == 0
    assert usage["completion_tokens"] == 0
    assert usage["cost_usd"] == 0.0


# ── budget kill-switch integration ───────────────────────────────────────────

def test_generate_blocks_when_budget_exhausted(redis_client, monkeypatch):
    """Once monthly cap is hit, generate() must NOT call any provider."""
    monkeypatch.setenv("LLM_BUDGET_CAP_CENTS", "1")  # $0.01 cap
    budget.record_cost(redis_client, 0.02)  # spend $0.02 — over cap

    oc = MagicMock()
    ac = MagicMock()
    with pytest.raises(budget.BudgetExhausted):
        llm.generate(redis_client, "italy", "PROMPT", oc, ac)
    oc.chat.completions.create.assert_not_called()
    ac.messages.create.assert_not_called()


def test_generate_accumulates_spend(redis_client):
    """Every successful call should bump the Redis bucket."""
    oc = _openai_client(VALID_PAYLOAD, 1000, 500)  # cost = $0.0075 = 0.75 cents
    llm.generate(redis_client, "italy", "P", oc, MagicMock())
    llm.generate(redis_client, "italy", "P", oc, MagicMock())
    # Two calls: each $0.0075 → round(0.75) = 1 cent recorded per call → 2 cents total.
    assert budget.current_spend_cents(redis_client) == 2


def test_generate_crosses_cap_then_next_call_blocks(redis_client, monkeypatch):
    """First call goes through; second call after the cap is hit must raise."""
    monkeypatch.setenv("LLM_BUDGET_CAP_CENTS", "1")  # $0.01 cap
    oc = _openai_client(VALID_PAYLOAD, 1000, 500)  # ~$0.0075 = 1 cent rounded

    # First call: succeeds, may cross cap.
    llm.generate(redis_client, "italy", "P", oc, MagicMock())

    # Second call: budget should now be exhausted.
    with pytest.raises(budget.BudgetExhausted):
        llm.generate(redis_client, "italy", "P", oc, MagicMock())
