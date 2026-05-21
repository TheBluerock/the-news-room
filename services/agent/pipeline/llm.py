"""LLM calls with per-market circuit breaker. Primary: OpenAI. Fallback: Anthropic.
Returns structured article data (title, excerpt, section, author, tags, body)
plus per-call token usage so downstream services can compute cost.
"""
import json
import logging
import re
import unicodedata
import uuid

import redis as redis_lib
from opentelemetry import trace
from prometheus_client import Counter, Gauge

from pipeline import budget, circuit

logger = logging.getLogger("agent.pipeline.llm")
tracer = trace.get_tracer("agent.pipeline.llm")

MARKET_LANGUAGE = {"italy": "Italian", "usa": "English", "china": "Simplified Chinese"}
ARTICLE_WORD_TARGET = 600

OPENAI_MODEL    = "gpt-4o"
ANTHROPIC_MODEL = "claude-sonnet-4-6"

# Static USD pricing per 1M tokens. Update when provider price lists change.
# Format: model -> (input_usd_per_1m, output_usd_per_1m). Sourced from openai.com/pricing
# and docs.anthropic.com — 2026-05.
PRICE_PER_MTOKEN = {
    OPENAI_MODEL:    (2.50, 10.00),
    ANTHROPIC_MODEL: (3.00, 15.00),
}


def cost_usd(model: str, prompt_tokens: int, completion_tokens: int) -> float:
    """Compute cost in USD for a given model + token split. 0.0 if model unknown."""
    in_rate, out_rate = PRICE_PER_MTOKEN.get(model, (0.0, 0.0))
    return (prompt_tokens / 1_000_000.0) * in_rate + (completion_tokens / 1_000_000.0) * out_rate


# ── Prometheus metrics ──────────────────────────────────────────────────────
llm_tokens_total = Counter(
    "llm_tokens_total",
    "Total LLM tokens consumed.",
    ["market", "model", "type"],  # type=prompt|completion
)
llm_cost_usd_total = Counter(
    "llm_cost_usd_total",
    "Cumulative USD spend on LLM API calls.",
    ["market", "model"],
)
llm_budget_remaining_cents = Gauge(
    "llm_budget_remaining_cents",
    "Cents remaining before monthly LLM budget kill-switch trips.",
)
llm_budget_kill_switch_active = Gauge(
    "llm_budget_kill_switch_active",
    "1 if the monthly budget has been exhausted and generation is paused.",
)

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
) -> tuple[dict, str, dict]:
    """
    Generate structured article data using available LLM provider.

    Returns (structured, article_id, usage_meta) where usage_meta is
        {prompt_tokens: int, completion_tokens: int, model: str, cost_usd: float}.

    Raises:
      budget.BudgetExhausted  — monthly cap hit; no provider was called.
      RuntimeError            — both circuits open after attempting both providers.
    """
    with tracer.start_as_current_span("llm.generate") as span:
        span.set_attribute("market", market)
        article_id = str(uuid.uuid4())

        # Hard gate: refuse to call any provider if the monthly cap is exhausted.
        budget.assert_within_budget(rdb)

        if circuit.is_available(rdb, market, "openai"):
            try:
                structured, usage = _call_openai(openai_client, market, prompt)
                _record_usage(rdb, market, usage)
                circuit.record_success(rdb, market, "openai")
                span.set_attribute("provider", "openai")
                return structured, article_id, usage
            except budget.BudgetExhausted:
                raise
            except Exception as e:
                logger.warning("openai failed market=%s: %s", market, e)
                circuit.record_failure(rdb, market, "openai")

        if circuit.is_available(rdb, market, "anthropic"):
            try:
                structured, usage = _call_anthropic(anthropic_client, market, prompt)
                _record_usage(rdb, market, usage)
                circuit.record_success(rdb, market, "anthropic")
                span.set_attribute("provider", "anthropic")
                return structured, article_id, usage
            except budget.BudgetExhausted:
                raise
            except Exception as e:
                logger.error("anthropic fallback failed market=%s: %s", market, e)
                circuit.record_failure(rdb, market, "anthropic")

        raise RuntimeError(f"both LLM circuits open for market={market}")


def _record_usage(rdb: redis_lib.Redis, market: str, usage: dict) -> None:
    """Update Redis monthly counter + Prometheus metrics for one LLM call."""
    model = usage["model"]
    pt = int(usage.get("prompt_tokens", 0))
    ct = int(usage.get("completion_tokens", 0))
    cost = float(usage.get("cost_usd", 0.0))

    llm_tokens_total.labels(market=market, model=model, type="prompt").inc(pt)
    llm_tokens_total.labels(market=market, model=model, type="completion").inc(ct)
    llm_cost_usd_total.labels(market=market, model=model).inc(cost)

    remaining = budget.record_cost(rdb, cost)
    llm_budget_remaining_cents.set(remaining)
    llm_budget_kill_switch_active.set(1 if remaining <= 0 else 0)


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


def _call_openai(client, market: str, prompt: str) -> tuple[dict, dict]:
    """Returns (structured_article, usage_meta)."""
    response = client.chat.completions.create(
        model=OPENAI_MODEL,
        response_format={"type": "json_object"},
        messages=[
            {"role": "system", "content": prompt},
            {"role": "user",   "content": _user_message(market)},
        ],
        max_tokens=1600,
        temperature=0.7,
    )
    structured = _parse_and_validate(response.choices[0].message.content.strip())

    usage_obj = getattr(response, "usage", None)
    pt = int(getattr(usage_obj, "prompt_tokens", 0) or 0)
    ct = int(getattr(usage_obj, "completion_tokens", 0) or 0)
    usage = {
        "prompt_tokens":     pt,
        "completion_tokens": ct,
        "model":             OPENAI_MODEL,
        "cost_usd":          cost_usd(OPENAI_MODEL, pt, ct),
    }
    return structured, usage


def _call_anthropic(client, market: str, prompt: str) -> tuple[dict, dict]:
    """Returns (structured_article, usage_meta)."""
    response = client.messages.create(
        model=ANTHROPIC_MODEL,
        max_tokens=1600,
        system=prompt,
        messages=[{"role": "user", "content": _user_message(market)}],
    )
    structured = _parse_and_validate(response.content[0].text.strip())

    usage_obj = getattr(response, "usage", None)
    # Anthropic SDK uses input_tokens / output_tokens.
    pt = int(getattr(usage_obj, "input_tokens", 0) or 0)
    ct = int(getattr(usage_obj, "output_tokens", 0) or 0)
    usage = {
        "prompt_tokens":     pt,
        "completion_tokens": ct,
        "model":             ANTHROPIC_MODEL,
        "cost_usd":          cost_usd(ANTHROPIC_MODEL, pt, ct),
    }
    return structured, usage
