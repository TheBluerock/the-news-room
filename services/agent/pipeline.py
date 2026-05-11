"""LangGraph article generation pipeline — wired to real infrastructure."""

from __future__ import annotations

import logging
from typing import TypedDict

import grpc
import redis as redis_lib
from confluent_kafka import Producer
from langgraph.graph import StateGraph
from opentelemetry import trace

from pipeline import (
    circuit,
    context,
    corrections,
    llm,
    memory,
    publisher,
    rate_limit,
    semantic,
)

logger = logging.getLogger("agent.pipeline")
tracer = trace.get_tracer("agent.pipeline")

MARKETS = ("italy", "usa", "china")

MARKET_SYSTEM_PROMPTS = {
    "italy": (
        "You are a formal Italian wine and food journalist. "
        "Use correct DOC/DOCG terminology. Prefer Italian terminology where appropriate. "
        "Tone: authoritative, regional expertise."
    ),
    "usa": (
        "You are an approachable American food and wine writer. "
        "Use 100-point scoring context where relevant. "
        "Tone: accessible, enthusiastic, consumer-friendly."
    ),
    "china": (
        "You are a luxury food and wine journalist writing for the Chinese market. "
        "Emphasise gift culture, prestige, and provenance. "
        "Tone: aspirational, respectful, culturally sensitive."
    ),
}


class ArticleState(TypedDict):
    market: str
    topic_id: str
    topic_name: str
    trace_id: str
    rdb: redis_lib.Redis
    learner_channel: grpc.Channel
    analytics_channel: grpc.Channel
    producer: Producer
    openai_client: object
    anthropic_client: object
    memory: dict
    corrections_data: dict
    context: list
    prompt: str
    content: str
    article_id: str


def build_graph() -> StateGraph:
    graph = StateGraph(ArticleState)

    graph.add_node("fetch_memory", _fetch_memory)
    graph.add_node("fetch_corrections", _fetch_corrections)
    graph.add_node("fetch_context", _fetch_context)
    graph.add_node("semantic_search", _semantic_search)
    graph.add_node("check_rate_limit", _check_rate_limit)
    graph.add_node("check_circuit", _check_circuit)
    graph.add_node("build_prompt", _build_prompt)
    graph.add_node("generate", _generate)
    graph.add_node("write_memory", _write_memory)
    graph.add_node("publish", _publish)

    graph.set_entry_point("fetch_memory")
    graph.add_edge("fetch_memory", "fetch_corrections")
    graph.add_edge("fetch_corrections", "fetch_context")
    graph.add_edge("fetch_context", "semantic_search")
    graph.add_edge("semantic_search", "check_rate_limit")
    graph.add_edge("check_rate_limit", "check_circuit")
    graph.add_edge("check_circuit", "build_prompt")
    graph.add_edge("build_prompt", "generate")
    graph.add_edge("generate", "write_memory")
    graph.add_edge("write_memory", "publish")
    graph.set_finish_point("publish")

    return graph.compile()


# ── Node implementations ──────────────────────────────────────────────────────

def _fetch_memory(state: ArticleState) -> dict:
    return {"memory": memory.fetch(state["rdb"], state["market"])}


def _fetch_corrections(state: ArticleState) -> dict:
    return {"corrections_data": corrections.fetch(state["rdb"], state["market"])}


def _fetch_context(state: ArticleState) -> dict:
    nodes = context.fetch(state["learner_channel"], state["market"], state["topic_name"])
    return {"context": nodes}


def _semantic_search(state: ArticleState) -> dict:
    results = semantic.search(state["rdb"], state["market"], None, state["topic_name"])
    return {"context": state.get("context", []) + results}


def _check_rate_limit(state: ArticleState) -> dict:
    if not rate_limit.acquire(state["rdb"], state["market"]):
        raise RuntimeError(f"LLM rate limit exceeded for market={state['market']}")
    return {}


def _check_circuit(state: ArticleState) -> dict:
    openai_ok = circuit.is_available(state["rdb"], state["market"], "openai")
    anthropic_ok = circuit.is_available(state["rdb"], state["market"], "anthropic")
    if not openai_ok and not anthropic_ok:
        raise RuntimeError(f"all LLM circuits open for market={state['market']}")
    return {}


def _build_prompt(state: ArticleState) -> dict:
    system = MARKET_SYSTEM_PROMPTS[state["market"]]
    corrs = state.get("corrections_data", {})
    ctx = state.get("context", [])

    parts = [system]
    if corrs:
        active = "\n".join(f"- {v.get('reason', v)}" for v in corrs.values())
        parts.append(f"\nACTIVE EDITORIAL CORRECTIONS:\n{active}")
    if ctx:
        snippets = "\n".join(f"- {n.get('content', '')[:150]}" for n in ctx[:3])
        parts.append(f"\nRELEVANT CONTEXT:\n{snippets}")

    return {"prompt": "\n".join(parts) + f"\n\nTopic: {state['topic_name']}"}


def _generate(state: ArticleState) -> dict:
    content, article_id = llm.generate(
        state["rdb"],
        state["market"],
        state["prompt"],
        state["openai_client"],
        state["anthropic_client"],
    )
    return {"content": content, "article_id": article_id}


def _write_memory(state: ArticleState) -> dict:
    summary = (state.get("content") or "")[:200]
    memory.write(state["rdb"], state["market"], state["topic_name"], summary)
    return {}


def _publish(state: ArticleState) -> dict:
    publisher.publish(
        state["producer"],
        state["market"],
        state["topic_id"],
        state["article_id"],
        state["content"],
        state["trace_id"],
    )
    return {}
