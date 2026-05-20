"""Pure unit tests for pipeline.py:_build_prompt + MARKET_SYSTEM_PROMPTS."""
from __future__ import annotations

import pytest

import graph as pipeline_mod


def _state(market="italy", **overrides):
    base = {
        "market": market,
        "topic_id": "t1",
        "topic_name": "Barolo 2018 vintage",
        "trace_id": "trace-1",
        "rdb": None,
        "learner_channel": None,
        "learner_rest_url": "",
        "analytics_channel": None,
        "producer": None,
        "openai_client": None,
        "anthropic_client": None,
        "memory": {},
        "corrections_data": {},
        "context": [],
        "quality_summary": {},
        "prompt": "",
        "content": "",
        "title": "",
        "excerpt": "",
        "section": "",
        "author": "",
        "tags": [],
        "slug": "",
        "article_id": "",
    }
    base.update(overrides)
    return base


@pytest.mark.parametrize("market, signature", [
    ("italy", "Italian wine and food journalist"),
    ("usa", "American food and wine writer"),
    ("china", "luxury food and wine journalist"),
])
def test_market_system_prompts_per_market(market, signature):
    assert signature in pipeline_mod.MARKET_SYSTEM_PROMPTS[market]


def test_market_system_prompts_keys_match_markets_tuple():
    assert set(pipeline_mod.MARKET_SYSTEM_PROMPTS.keys()) == set(pipeline_mod.MARKETS)


def test_build_prompt_minimal():
    out = pipeline_mod._build_prompt(_state())
    assert "prompt" in out
    prompt = out["prompt"]
    assert "Italian wine and food journalist" in prompt
    assert "Topic: Barolo 2018 vintage" in prompt
    # No optional sections appear when state is empty
    assert "ACTIVE EDITORIAL CORRECTIONS" not in prompt
    assert "RELEVANT CONTEXT" not in prompt
    assert "QUALITY SIGNAL" not in prompt


def test_build_prompt_includes_corrections():
    state = _state(corrections_data={
        "c1": {"reason": "avoid mentioning 2020 vintage"},
        "c2": {"reason": "use formal Italian register"},
    })
    out = pipeline_mod._build_prompt(state)
    prompt = out["prompt"]
    assert "ACTIVE EDITORIAL CORRECTIONS" in prompt
    assert "avoid mentioning 2020 vintage" in prompt
    assert "use formal Italian register" in prompt


def test_build_prompt_corrections_payload_without_reason():
    # corrections.fetch may return dicts without "reason" key — code falls back to repr.
    state = _state(corrections_data={"c1": {"freeform": "some note"}})
    out = pipeline_mod._build_prompt(state)
    # Either the dict repr or the value appears — should not raise.
    assert "ACTIVE EDITORIAL CORRECTIONS" in out["prompt"]


def test_build_prompt_includes_context_top_3():
    ctx = [
        {"content": f"context snippet number {i} " + "x" * 200}
        for i in range(5)
    ]
    state = _state(context=ctx)
    out = pipeline_mod._build_prompt(state)
    prompt = out["prompt"]
    assert "RELEVANT CONTEXT" in prompt
    assert "context snippet number 0" in prompt
    assert "context snippet number 2" in prompt
    # Capped to top-3
    assert "context snippet number 3" not in prompt
    # Snippet length capped to 150 chars
    line_with_zero = [ln for ln in prompt.splitlines() if "context snippet number 0" in ln][0]
    assert len(line_with_zero) <= 155  # "- " prefix (2) + 150 chars slice + small margin


def test_build_prompt_includes_quality_signal_with_rejections():
    state = _state(quality_summary={
        "avg_quality_score": 0.73,
        "top_rejections": ["factual error", "tone too casual", "missing source", "ignored"],
    })
    out = pipeline_mod._build_prompt(state)
    prompt = out["prompt"]
    assert "QUALITY SIGNAL" in prompt
    assert "0.73/1.0" in prompt
    assert "factual error" in prompt
    assert "tone too casual" in prompt
    assert "missing source" in prompt
    # 4th rejection is dropped (top-3 only)
    assert "ignored" not in prompt


def test_build_prompt_quality_signal_no_rejections():
    state = _state(quality_summary={"avg_quality_score": 0.9, "top_rejections": []})
    out = pipeline_mod._build_prompt(state)
    prompt = out["prompt"]
    assert "QUALITY SIGNAL" in prompt
    assert "0.90/1.0" in prompt
    assert "Avoid:" not in prompt


def test_build_prompt_quality_signal_default_score():
    # Empty summary triggers default avg_quality_score=0.5
    state = _state(quality_summary={})
    out = pipeline_mod._build_prompt(state)
    # Empty dict is falsy → entire block skipped
    assert "QUALITY SIGNAL" not in out["prompt"]


def test_build_prompt_unknown_market_raises_keyerror():
    # Defensive: unknown market should fail loud (no fallback prompt).
    with pytest.raises(KeyError):
        pipeline_mod._build_prompt(_state(market="japan"))


def test_build_prompt_topic_appended_last():
    out = pipeline_mod._build_prompt(_state(topic_name="My Topic"))
    assert out["prompt"].rstrip().endswith("Topic: My Topic")
