"""LangGraph article generation pipeline for the Agent service."""

from __future__ import annotations

from typing import TypedDict

from langgraph.graph import StateGraph

MARKETS = ("italy", "usa", "china")

MARKET_SYSTEM_PROMPTS = {
    "italy": (
        "You are a formal Italian wine and food journalist. "
        "Use correct DOC/DOCG/DOCG terminology. Prefer Italian terminology where appropriate. "
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
    memory: dict
    corrections: dict
    context: list
    analytics: dict
    prompt: str
    content: str
    article_id: str


def build_graph() -> StateGraph:
    graph = StateGraph(ArticleState)

    graph.add_node("fetch_memory", fetch_redis_short_term_memory)
    graph.add_node("fetch_corrections", fetch_fast_path_corrections)   # 48h TTL
    graph.add_node("fetch_context", fetch_knowledge_graph)             # gRPC → Learner
    graph.add_node("semantic_search", search_redis_hnsw_skip_stale)    # HNSW
    graph.add_node("fetch_analytics", get_trending_signals)            # gRPC → Analytics
    graph.add_node("check_rate_limit", acquire_llm_token_bucket)
    graph.add_node("check_circuit", check_llm_circuit_breaker)
    graph.add_node("build_prompt", construct_market_prompt)
    graph.add_node("generate", call_llm_with_fallback)
    graph.add_node("write_memory", update_redis_short_term_memory)
    graph.add_node("publish", emit_article_generated)

    graph.set_entry_point("fetch_memory")
    graph.add_edge("fetch_memory", "fetch_corrections")
    graph.add_edge("fetch_corrections", "fetch_context")
    graph.add_edge("fetch_context", "semantic_search")
    graph.add_edge("semantic_search", "fetch_analytics")
    graph.add_edge("fetch_analytics", "check_rate_limit")
    graph.add_edge("check_rate_limit", "check_circuit")
    graph.add_edge("check_circuit", "build_prompt")
    graph.add_edge("build_prompt", "generate")
    graph.add_edge("generate", "write_memory")
    graph.add_edge("write_memory", "publish")
    graph.set_finish_point("publish")

    return graph.compile()


# ── Node stubs ────────────────────────────────────────────────────────────────

def fetch_redis_short_term_memory(state: ArticleState) -> dict:
    # TODO: HGETALL memory:<market>
    return {"memory": {}}


def fetch_fast_path_corrections(state: ArticleState) -> dict:
    # TODO: GET corrections:<market>  (48h TTL key)
    return {"corrections": {}}


def fetch_knowledge_graph(state: ArticleState) -> dict:
    # TODO: gRPC LearnerService.QueryKnowledgeGraph
    return {"context": []}


def search_redis_hnsw_skip_stale(state: ArticleState) -> dict:
    # TODO: Redis HNSW KNN search on vectors:<market>:*
    # Skip entries where vector:stale:<article_id> exists
    return {}


def get_trending_signals(state: ArticleState) -> dict:
    # TODO: gRPC AnalyticsService.GetTrendingSignals
    return {"analytics": {}}


def acquire_llm_token_bucket(state: ArticleState) -> dict:
    # TODO: Redis token bucket DECR llm:rate:<market>
    return {}


def check_llm_circuit_breaker(state: ArticleState) -> dict:
    # TODO: check circuit state — llm_circuit_state{market, provider}
    # Open → fail fast, queue to DLQ with backoff
    # Half-open → allow one probe
    return {}


def construct_market_prompt(state: ArticleState) -> dict:
    system = MARKET_SYSTEM_PROMPTS[state["market"]]
    corrections = state.get("corrections", {})
    active_corrections = ""
    if corrections:
        active_corrections = f"\n\nACTIVE CORRECTIONS:\n{corrections}"
    prompt = f"{system}{active_corrections}\n\nTopic: {state['topic_name']}"
    return {"prompt": prompt}


def call_llm_with_fallback(state: ArticleState) -> dict:
    # TODO: primary → OpenAI GPT-4o, fallback → Anthropic claude-sonnet-4-6
    # Wrap with circuit breaker per market
    return {"content": "", "article_id": ""}


def update_redis_short_term_memory(state: ArticleState) -> dict:
    # TODO: HSET memory:<market> topic_name ... updated_at ...  EXPIRE 604800
    return {}


def emit_article_generated(state: ArticleState) -> dict:
    # TODO: validate against infra/schemas/article.generated.json
    # TODO: produce to RedPanda topic article.generated with W3C TraceContext header
    return {}
