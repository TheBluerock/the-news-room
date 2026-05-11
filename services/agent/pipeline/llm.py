"""LLM calls with per-market circuit breaker. Primary: OpenAI. Fallback: Anthropic.
Returns structured article data (title, excerpt, section, author, tags, body).
"""
import json
import logging
import re
import unicodedata
import uuid

import redis as redis_lib
from opentelemetry import trace

from pipeline import circuit

logger = logging.getLogger("agent.pipeline.llm")
tracer = trace.get_tracer("agent.pipeline.llm")

MARKET_LANGUAGE = {"italy": "Italian", "usa": "English", "china": "Simplified Chinese"}
ARTICLE_WORD_TARGET = 600

VALID_SECTIONS = [
    "degustazioni", "cantine", "itinerari", "territori",
    "abbinamenti", "eventi", "interviste", "guida", "sostenibilita",
]

_JSON_SCHEMA = """{
  "title": "<article headline, max 15 words>",
  "excerpt": "<lead paragraph, max 35 words>",
  "section": "<one of: degustazioni|cantine|itinerari|territori|abbinamenti|eventi|interviste|guida|sostenibilita>",
  "author": "<journalist name in local style>",
  "tags": ["<tag1>", "<tag2>", "<tag3>", "<tag4>", "<tag5>"],
  "body": "<full article body, ~600 words, paragraphs separated by double newline>"
}"""


def slugify(text: str) -> str:
    text = unicodedata.normalize("NFKD", text).encode("ascii", "ignore").decode()
    text = re.sub(r"[^a-z0-9]+", "-", text.lower()).strip("-")
    return text[:80]


def generate(
    rdb: redis_lib.Redis,
    market: str,
    prompt: str,
    openai_client,
    anthropic_client,
) -> tuple[dict, str]:
    """
    Generate structured article data using available LLM provider.
    Returns (structured: dict, article_id: str).
    Raises RuntimeError if both circuits are open.
    """
    with tracer.start_as_current_span("llm.generate") as span:
        span.set_attribute("market", market)
        article_id = str(uuid.uuid4())

        if circuit.is_available(rdb, market, "openai"):
            try:
                structured = _call_openai(openai_client, market, prompt)
                circuit.record_success(rdb, market, "openai")
                span.set_attribute("provider", "openai")
                return structured, article_id
            except Exception as e:
                logger.warning("openai failed market=%s: %s", market, e)
                circuit.record_failure(rdb, market, "openai")

        if circuit.is_available(rdb, market, "anthropic"):
            try:
                structured = _call_anthropic(anthropic_client, market, prompt)
                circuit.record_success(rdb, market, "anthropic")
                span.set_attribute("provider", "anthropic")
                return structured, article_id
            except Exception as e:
                logger.error("anthropic fallback failed market=%s: %s", market, e)
                circuit.record_failure(rdb, market, "anthropic")

        raise RuntimeError(f"both LLM circuits open for market={market}")


def _user_message(market: str) -> str:
    lang = MARKET_LANGUAGE.get(market, "English")
    return (
        f"Write a {ARTICLE_WORD_TARGET}-word article in {lang} about the topic. "
        f"Return ONLY valid JSON matching this schema exactly:\n{_JSON_SCHEMA}"
    )


def _parse_and_validate(raw: str) -> dict:
    raw = raw.strip()
    # Strip markdown code fences if present
    if raw.startswith("```"):
        raw = re.sub(r"^```[a-z]*\n?", "", raw).rstrip("`").strip()
    data = json.loads(raw)
    if data.get("section") not in VALID_SECTIONS:
        data["section"] = "territori"
    data.setdefault("tags", [])
    return data


def _call_openai(client, market: str, prompt: str) -> dict:
    response = client.chat.completions.create(
        model="gpt-4o",
        response_format={"type": "json_object"},
        messages=[
            {"role": "system", "content": prompt},
            {"role": "user",   "content": _user_message(market)},
        ],
        max_tokens=1600,
        temperature=0.7,
    )
    return _parse_and_validate(response.choices[0].message.content.strip())


def _call_anthropic(client, market: str, prompt: str) -> dict:
    response = client.messages.create(
        model="claude-sonnet-4-6",
        max_tokens=1600,
        system=prompt,
        messages=[{"role": "user", "content": _user_message(market)}],
    )
    return _parse_and_validate(response.content[0].text.strip())
