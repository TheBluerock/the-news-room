"""Integration tests for pipeline.llm.generate() — real Redis circuit, mocked LLM clients."""
from __future__ import annotations

import json
from types import SimpleNamespace
from unittest.mock import MagicMock

import pytest

from pipeline import circuit, llm


VALID_PAYLOAD = {
    "title": "Barolo 2018",
    "excerpt": "lead paragraph",
    "section": "cantine",
    "author": "Maria Rossi",
    "tags": ["barolo", "piedmont", "docg", "2018", "tasting"],
    "body": "Body text " * 50,
}


def _openai_client_returning(payload_dict: dict):
    client = MagicMock()
    raw = json.dumps(payload_dict)
    response = SimpleNamespace(
        choices=[SimpleNamespace(message=SimpleNamespace(content=raw))],
    )
    client.chat.completions.create.return_value = response
    return client


def _anthropic_client_returning(payload_dict: dict):
    client = MagicMock()
    raw = json.dumps(payload_dict)
    response = SimpleNamespace(content=[SimpleNamespace(text=raw)])
    client.messages.create.return_value = response
    return client


def _failing_openai():
    client = MagicMock()
    client.chat.completions.create.side_effect = RuntimeError("openai 500")
    return client


def _failing_anthropic():
    client = MagicMock()
    client.messages.create.side_effect = RuntimeError("anthropic 500")
    return client


def test_openai_happy_path(redis_client):
    oc = _openai_client_returning(VALID_PAYLOAD)
    ac = MagicMock()  # never called
    structured, article_id, usage = llm.generate(redis_client, "italy", "PROMPT", oc, ac)
    assert structured["title"] == "Barolo 2018"
    assert structured["section"] == "cantine"
    assert article_id  # uuid populated
    ac.messages.create.assert_not_called()
    # Circuit success path → closed state
    assert circuit.get_state(redis_client, "italy", "openai") == circuit.STATE_CLOSED
    # Usage meta is populated even when the mock response has no `usage` attr
    # (defaults to zero token counts, zero cost, but model name still set).
    assert isinstance(usage, dict)
    assert set(usage.keys()) >= {"model", "prompt_tokens", "completion_tokens", "cost_usd"}
    assert isinstance(usage["model"], str) and usage["model"]
    assert isinstance(usage["prompt_tokens"], int)
    assert isinstance(usage["completion_tokens"], int)
    assert isinstance(usage["cost_usd"], (int, float))


def test_openai_failure_falls_back_to_anthropic(redis_client):
    oc = _failing_openai()
    ac = _anthropic_client_returning(VALID_PAYLOAD)
    structured, article_id, _usage = llm.generate(redis_client, "italy", "PROMPT", oc, ac)
    assert structured["title"] == "Barolo 2018"
    assert article_id
    # OpenAI failure recorded, Anthropic success closed
    assert int(redis_client.get("circuit:italy:openai:failures")) == 1
    assert circuit.get_state(redis_client, "italy", "anthropic") == circuit.STATE_CLOSED


def test_both_providers_fail_raises_runtime(redis_client):
    oc = _failing_openai()
    ac = _failing_anthropic()
    with pytest.raises(RuntimeError):
        llm.generate(redis_client, "italy", "PROMPT", oc, ac)
    # Both providers recorded a failure
    assert int(redis_client.get("circuit:italy:openai:failures")) == 1
    assert int(redis_client.get("circuit:italy:anthropic:failures")) == 1


def test_both_circuits_open_raises_runtime(redis_client):
    # Pre-trip both circuits
    for _ in range(circuit.FAILURE_THRESHOLD):
        circuit.record_failure(redis_client, "italy", "openai")
        circuit.record_failure(redis_client, "italy", "anthropic")
    oc = MagicMock()
    ac = MagicMock()
    with pytest.raises(RuntimeError, match="both LLM circuits open"):
        llm.generate(redis_client, "italy", "PROMPT", oc, ac)
    # Clients never called when circuits are open
    oc.chat.completions.create.assert_not_called()
    ac.messages.create.assert_not_called()


def test_openai_circuit_open_skips_to_anthropic(redis_client):
    for _ in range(circuit.FAILURE_THRESHOLD):
        circuit.record_failure(redis_client, "italy", "openai")
    oc = MagicMock()  # should not be called
    ac = _anthropic_client_returning(VALID_PAYLOAD)
    structured, _, _ = llm.generate(redis_client, "italy", "PROMPT", oc, ac)
    assert structured["title"] == "Barolo 2018"
    oc.chat.completions.create.assert_not_called()


def test_invalid_section_coerced_to_territori(redis_client):
    payload = {**VALID_PAYLOAD, "section": "not-a-real-section"}
    oc = _openai_client_returning(payload)
    structured, _, _ = llm.generate(redis_client, "italy", "PROMPT", oc, MagicMock())
    assert structured["section"] == "territori"


def test_article_id_is_unique_per_call(redis_client):
    oc = _openai_client_returning(VALID_PAYLOAD)
    ids = set()
    for _ in range(10):
        _, aid, _ = llm.generate(redis_client, "italy", "PROMPT", oc, MagicMock())
        ids.add(aid)
    assert len(ids) == 10


def test_repeated_openai_failures_trip_circuit(redis_client):
    oc = _failing_openai()
    ac = _anthropic_client_returning(VALID_PAYLOAD)
    for _ in range(circuit.FAILURE_THRESHOLD):
        llm.generate(redis_client, "italy", "PROMPT", oc, ac)
    assert circuit.get_state(redis_client, "italy", "openai") == circuit.STATE_OPEN

    # Next call must skip OpenAI entirely
    oc.chat.completions.create.reset_mock()
    llm.generate(redis_client, "italy", "PROMPT", oc, ac)
    oc.chat.completions.create.assert_not_called()
