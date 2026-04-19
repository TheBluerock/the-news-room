"""LLM calls with per-market circuit breaker. Primary: OpenAI. Fallback: Anthropic."""
import logging
import time
import uuid

import redis as redis_lib
from opentelemetry import trace

from pipeline import circuit

logger = logging.getLogger("agent.pipeline.llm")
tracer = trace.get_tracer("agent.pipeline.llm")

MARKET_LANGUAGE = {"italy": "Italian", "usa": "English", "china": "Simplified Chinese"}
ARTICLE_WORD_TARGET = 600


def generate(
    rdb: redis_lib.Redis,
    market: str,
    prompt: str,
    openai_client,
    anthropic_client,
) -> tuple[str, str]:
    """
    Generate article content using available LLM provider.
    Returns (content, article_id).
    Raises RuntimeError if both circuits are open.
    """
    with tracer.start_as_current_span("llm.generate") as span:
        span.set_attribute("market", market)
        article_id = str(uuid.uuid4())

        # Try OpenAI first
        if circuit.is_available(rdb, market, "openai"):
            try:
                content = _call_openai(openai_client, market, prompt)
                circuit.record_success(rdb, market, "openai")
                span.set_attribute("provider", "openai")
                return content, article_id
            except Exception as e:
                logger.warning("openai failed market=%s: %s", market, e)
                circuit.record_failure(rdb, market, "openai")

        # Fallback to Anthropic
        if circuit.is_available(rdb, market, "anthropic"):
            try:
                content = _call_anthropic(anthropic_client, market, prompt)
                circuit.record_success(rdb, market, "anthropic")
                span.set_attribute("provider", "anthropic")
                return content, article_id
            except Exception as e:
                logger.error("anthropic fallback failed market=%s: %s", market, e)
                circuit.record_failure(rdb, market, "anthropic")

        raise RuntimeError(f"both LLM circuits open for market={market}")


def _call_openai(client, market: str, prompt: str) -> str:
    lang = MARKET_LANGUAGE.get(market, "English")
    response = client.chat.completions.create(
        model="gpt-4o",
        messages=[
            {"role": "system", "content": prompt},
            {"role": "user", "content": f"Write a {ARTICLE_WORD_TARGET}-word article in {lang}."},
        ],
        max_tokens=1200,
        temperature=0.7,
    )
    return response.choices[0].message.content.strip()


def _call_anthropic(client, market: str, prompt: str) -> str:
    lang = MARKET_LANGUAGE.get(market, "English")
    response = client.messages.create(
        model="claude-sonnet-4-6",
        max_tokens=1200,
        system=prompt,
        messages=[
            {"role": "user", "content": f"Write a {ARTICLE_WORD_TARGET}-word article in {lang}."},
        ],
    )
    return response.content[0].text.strip()
