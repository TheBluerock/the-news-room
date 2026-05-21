"""Unit tests for checks.py — mocks OpenAI client, no I/O."""
from __future__ import annotations

import importlib
import json
import sys
from types import SimpleNamespace
from unittest.mock import MagicMock

import pytest

# conftest.py stubs `checks` for other tests. Force-reload the real module here.
sys.modules.pop("checks", None)
checks = importlib.import_module("checks")


def _openai_returning(payload: dict):
    client = MagicMock()
    raw = json.dumps(payload)
    client.chat.completions.create.return_value = SimpleNamespace(
        choices=[SimpleNamespace(message=SimpleNamespace(content=raw))],
    )
    return client


def _failing_openai():
    client = MagicMock()
    client.chat.completions.create.side_effect = RuntimeError("openai 500")
    return client


# ── check_cultural ──────────────────────────────────────────────────────────

@pytest.mark.parametrize("market", ["italy", "usa", "china"])
def test_check_cultural_per_market_pass(market):
    client = _openai_returning({"passed": True, "reason": ""})
    passed, reason = checks.check_cultural("article body", market, client)
    assert passed is True
    assert reason == ""
    # Verify the market-specific system prompt was selected.
    expected = {
        "italy": "Italian wine journalism",
        "usa":   "American food and wine journalism",
        "china": "Chinese luxury goods journalism",
    }[market]
    sent = client.chat.completions.create.call_args.kwargs["messages"][0]["content"]
    assert expected in sent


def test_check_cultural_unknown_market_defaults_to_usa():
    client = _openai_returning({"passed": True, "reason": ""})
    _ = checks.check_cultural("body", "japan", client)
    sent = client.chat.completions.create.call_args.kwargs["messages"][0]["content"]
    assert "American food and wine journalism" in sent


def test_check_cultural_failure_returns_reason():
    client = _openai_returning({"passed": False, "reason": "uses outdated DOC term"})
    passed, reason = checks.check_cultural("body", "italy", client)
    assert passed is False
    assert "outdated DOC term" in reason


def test_check_cultural_swallows_errors_and_passes():
    """Defensive: LLM error must NOT block moderation pipeline."""
    client = _failing_openai()
    passed, reason = checks.check_cultural("body", "italy", client)
    assert passed is True
    assert "check skipped" in reason


def test_check_cultural_truncates_content_to_3000_chars():
    client = _openai_returning({"passed": True, "reason": ""})
    long_content = "x" * 10_000
    _ = checks.check_cultural(long_content, "italy", client)
    user_msg = client.chat.completions.create.call_args.kwargs["messages"][1]["content"]
    # "Check this article:\n\n" prefix + 3000 chars body
    assert user_msg.count("x") == 3000


def test_check_cultural_missing_passed_field_defaults_true():
    client = _openai_returning({"reason": "no verdict"})
    passed, _ = checks.check_cultural("body", "italy", client)
    assert passed is True


# ── check_factual ───────────────────────────────────────────────────────────

def test_check_factual_happy_path():
    client = _openai_returning({"passed": True, "accuracy_score": 0.92, "issues": []})
    passed, score, issues = checks.check_factual("body", "italy", client)
    assert passed is True
    assert score == 0.92
    assert issues == []


def test_check_factual_with_issues():
    client = _openai_returning({
        "passed": False, "accuracy_score": 0.4,
        "issues": ["wrong vintage year", "invented producer"],
    })
    passed, score, issues = checks.check_factual("body", "italy", client)
    assert passed is False
    assert score == 0.4
    assert "wrong vintage year" in issues


def test_check_factual_swallows_errors_and_passes_with_default():
    client = _failing_openai()
    passed, score, issues = checks.check_factual("body", "italy", client)
    assert passed is True
    assert score == 0.8
    assert issues == []


def test_check_factual_score_coerced_to_float():
    client = _openai_returning({"passed": True, "accuracy_score": 1, "issues": []})
    _, score, _ = checks.check_factual("body", "italy", client)
    assert isinstance(score, float)
    assert score == 1.0


def test_check_factual_default_score_when_missing():
    client = _openai_returning({"passed": True, "issues": []})
    _, score, _ = checks.check_factual("body", "italy", client)
    assert score == 0.8


@pytest.mark.parametrize("market", ["italy", "usa", "china"])
def test_check_factual_market_injected_in_user_message(market):
    client = _openai_returning({"passed": True, "accuracy_score": 0.9, "issues": []})
    _ = checks.check_factual("body", market, client)
    user_msg = client.chat.completions.create.call_args.kwargs["messages"][1]["content"]
    assert f"{market} wine article" in user_msg


# ── score_quality ───────────────────────────────────────────────────────────

@pytest.mark.parametrize("words, expected", [
    (50, 0.3),
    (199, 0.3),
    (300, 0.6),
    (500, 0.8),
    (700, 0.8),
    (900, 0.95),
])
def test_score_quality_buckets(words, expected):
    content = " ".join(["word"] * words)
    assert checks.score_quality(content) == expected


def test_score_quality_empty():
    assert checks.score_quality("") == 0.3


def test_score_quality_boundary_at_200_words():
    # words=200 exactly is NOT <200, falls into next bucket (200-399 = 0.6).
    content = " ".join(["w"] * 200)
    assert checks.score_quality(content) == 0.6


def test_score_quality_boundary_at_800_words():
    # words=800 exactly is NOT >800; falls into 400-800 bucket → 0.8.
    content = " ".join(["w"] * 800)
    assert checks.score_quality(content) == 0.8
