"""Pure unit tests for pipeline/llm.py — no I/O, no Redis, no LLM."""
from __future__ import annotations

import pytest

from pipeline import llm


# ── slugify ─────────────────────────────────────────────────────────────────

@pytest.mark.parametrize("text, expected", [
    ("Hello World", "hello-world"),
    ("Barolo DOCG 2018: Vintage Notes", "barolo-docg-2018-vintage-notes"),
    ("Già fatto: à è ì ò ù", "gia-fatto-a-e-i-o-u"),
    ("   leading/trailing   ", "leading-trailing"),
    ("multiple---dashes", "multiple-dashes"),
    ("123 numeric start", "123-numeric-start"),
])
def test_slugify_basic(text, expected):
    assert llm.slugify(text) == expected


def test_slugify_chinese_strips_to_empty():
    # ASCII-only encoding drops CJK chars; result is empty after cleanup.
    assert llm.slugify("中文标题") == ""


def test_slugify_max_length_80():
    long = "a" * 200
    out = llm.slugify(long)
    assert len(out) <= 80


def test_slugify_only_punctuation_returns_empty():
    assert llm.slugify("!@#$%^&*()") == ""


# ── _parse_and_validate ─────────────────────────────────────────────────────

def test_parse_valid_json():
    raw = '{"title":"x","section":"degustazioni","body":"y","tags":["a"]}'
    data = llm._parse_and_validate(raw)
    assert data["title"] == "x"
    assert data["section"] == "degustazioni"
    assert data["tags"] == ["a"]


def test_parse_strips_markdown_fence():
    raw = '```json\n{"title":"x","section":"cantine","body":"y"}\n```'
    data = llm._parse_and_validate(raw)
    assert data["title"] == "x"


def test_parse_strips_plain_fence():
    raw = '```\n{"title":"x","section":"cantine","body":"y"}\n```'
    data = llm._parse_and_validate(raw)
    assert data["section"] == "cantine"


def test_parse_invalid_section_defaults_to_territori():
    raw = '{"title":"x","section":"NOT_A_SECTION","body":"y"}'
    data = llm._parse_and_validate(raw)
    assert data["section"] == "territori"


@pytest.mark.parametrize("section", [
    "degustazioni", "cantine", "itinerari", "territori",
    "abbinamenti", "eventi", "interviste", "guida", "sostenibilita",
])
def test_parse_all_valid_sections_pass_through(section):
    raw = f'{{"title":"x","section":"{section}","body":"y"}}'
    data = llm._parse_and_validate(raw)
    assert data["section"] == section


def test_parse_missing_tags_defaults_to_empty_list():
    raw = '{"title":"x","section":"cantine","body":"y"}'
    data = llm._parse_and_validate(raw)
    assert data["tags"] == []


def test_parse_malformed_json_raises():
    with pytest.raises(Exception):
        llm._parse_and_validate("{not json")


# ── _user_message ───────────────────────────────────────────────────────────

@pytest.mark.parametrize("market, expected_lang", [
    ("italy", "Italian"),
    ("usa", "English"),
    ("china", "Simplified Chinese"),
])
def test_user_message_language_per_market(market, expected_lang):
    msg = llm._user_message(market)
    assert expected_lang in msg
    assert "JSON" in msg


def test_user_message_unknown_market_defaults_english():
    msg = llm._user_message("japan")
    assert "English" in msg


def test_user_message_includes_schema():
    msg = llm._user_message("italy")
    # All required JSON keys present in schema string
    for key in ("title", "excerpt", "section", "author", "tags", "body"):
        assert key in msg
