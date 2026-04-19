# 05 — Agent Pipeline

## Overview

The agent service runs a LangGraph `StateGraph` pipeline triggered by `topic.trending` events. Each trigger produces one `article.generated` event. Max 2 concurrent runs per market (enforced by a `threading.Semaphore`).

The pipeline is stateless between runs — all state lives in `ArticleState`, a TypedDict passed through each node. Infrastructure clients (Redis, gRPC channels, LLM clients, Kafka producer) are injected into state at pipeline entry, not imported globally.

---

## ArticleState

```python
class ArticleState(TypedDict):
    # Trigger data
    market: str
    topic_id: str
    topic_name: str
    trace_id: str
    article_id: str

    # Infrastructure (injected at entry)
    rdb: Redis
    learner_channel: grpc.Channel
    analytics_channel: grpc.Channel
    openai_client: OpenAI
    anthropic_client: Anthropic
    producer: Producer

    # Pipeline results (populated as nodes run)
    memory: dict
    corrections: dict
    context: list[str]
    similar_articles: list[str]
    rate_limited: bool
    circuit_open: bool
    prompt: str
    content: str
    error: str | None
```

---

## Pipeline Nodes

| # | Node | Description |
|---|------|-------------|
| 1 | `fetch_memory` | HGETALL `memory:<market>` from Redis |
| 2 | `fetch_corrections` | SCAN `corrections:<market>:*`, returns all active correction dicts |
| 3 | `fetch_context` | gRPC `LearnerService.QueryKnowledgeGraph`, 5s timeout, empty fallback on error |
| 4 | `semantic_search` | Redis HNSW `FT.SEARCH` on `idx:articles:<market>`, skips `vector:stale:*` keys |
| 5 | `check_rate_limit` | Token bucket check; sets `rate_limited=True` if empty, does not consume token |
| 6 | `check_circuit` | Reads circuit state for both providers; sets `circuit_open=True` if both OPEN |
| 7 | `build_prompt` | Assembles system prompt from market personality + memory + corrections + context |
| 8 | `generate` | Calls LLM (OpenAI → Anthropic fallback), records circuit success/failure |
| 9 | `write_memory` | HSET `memory:<market>` with current topic/source/angle, reset 7-day TTL |
| 10 | `publish` | Validates schema, injects W3C trace headers, produces `article.generated` |

**Conditional exits:**
- After `check_rate_limit`: if `rate_limited`, route to `publish_queued` (exponential backoff queue)
- After `check_circuit`: if `circuit_open`, route to `publish_queued`
- After `generate`: if `error`, route to `publish_queued`

`publish_queued` implements: 60s → 5m → 30m backoff, then DLQ if all attempts fail.

---

## Market Personalities

Each market uses a distinct system prompt prefix. These are constants in `services/agent/pipeline/llm.py`.

### Italy
```
You are a senior wine journalist writing for an Italian readership.
Use formal register. Reference DOC/DOCG appellations accurately.
Emphasize regional terroir, producer heritage, and vintage character.
Avoid generic wine-tourism language.
```

### USA
```
You are a wine writer for an American general audience.
Be approachable and direct. Use 100-point scoring context where relevant
(e.g. Wine Spectator, Wine Advocate scale). Prioritize accessibility and
value-for-money framing. Avoid jargon without explanation.
```

### China
```
You are a luxury lifestyle writer covering fine wine for the Chinese market.
Frame content around gifting occasions, brand prestige, and investment value.
Acknowledge import regulations and duty context where relevant.
Use aspirational tone appropriate for high-net-worth readers.
```

> Market personalities are loaded into `ArticleState.prompt` by `build_prompt`. They are never mixed — an article is always generated for exactly one market.

---

## LLM Circuit Breaker

Per-market, per-provider. State stored in Redis. Prometheus metric: `llm_circuit_state{market, provider}`.

**Startup behavior:** On agent startup, circuit state is read from Redis. If the keys exist (from a previous run), the agent respects whatever state they hold — including OPEN. If the keys don't exist (first run, or Redis was wiped), all circuits initialize to CLOSED with zero failures. This means a crashed agent that left circuits OPEN will not accidentally reset them on restart.

### State Machine

```
CLOSED ──(≥5 failures in 60s)──► OPEN
OPEN   ──(30s elapsed)──────────► HALF_OPEN
HALF_OPEN ──(success)───────────► CLOSED
HALF_OPEN ──(failure)───────────► OPEN
```

### Thresholds

| Parameter | Value |
|-----------|-------|
| Failure threshold | 5 failures |
| Failure window | 60 seconds |
| Half-open probe delay | 30 seconds after trip |

### Redis Keys

| Key | Description |
|-----|-------------|
| `circuit:<market>:<provider>:state` | `0`=CLOSED, `1`=OPEN, `2`=HALF_OPEN |
| `circuit:<market>:<provider>:failures` | Count with 60s TTL (auto-resets window) |
| `circuit:<market>:<provider>:opened_at` | Unix timestamp of last trip |

### Fallback Sequence

1. Try OpenAI GPT-4o (if circuit CLOSED or HALF_OPEN)
2. On failure: record failure, try Anthropic claude-sonnet-4-6 (if that circuit CLOSED or HALF_OPEN)
3. On both OPEN: add to exponential backoff queue
4. Backoff exhausted: send to `article.generated.dlq`

---

## Rate Limiter (Token Bucket)

Per-market. Prevents LLM budget explosions.

| Parameter | Value |
|-----------|-------|
| Bucket capacity | 10 tokens |
| Refill rate | 1 token / minute |
| Concurrency control | Redis WATCH/MULTI/EXEC (optimistic locking) |

If `check_rate_limit` finds an empty bucket, the pipeline routes to `publish_queued` rather than failing. The token is consumed in `generate`, not `check_rate_limit`, to avoid consuming a token on a run that later fails circuit check.

---

## Semaphore (Concurrency Limit)

```python
_market_semaphores = {
    "italy": threading.Semaphore(2),
    "usa":   threading.Semaphore(2),
    "china": threading.Semaphore(2),
}
```

`topic.trending` consumer acquires the semaphore for the event's market before launching the pipeline. If both slots are occupied, the consumer blocks until one frees — it does not skip or drop the event.

**Trade-off:** Blocking preserves every trending signal at the cost of consumer lag. If articles take 3+ minutes each and the analytics publisher emits faster than 2 articles per market per window, the consumer's prefetch buffer will fill and consumer group lag will grow. This is acceptable at current scale (10 articles/market/day = ~1 article/90min). If the rate increases, consider increasing concurrency to 3–4 alongside the existing rate limiter, which already caps LLM spend.

---

## Corrections Injection

Active corrections (from `corrections:<market>:*` Redis keys) are injected into the system prompt by `build_prompt`:

```
[EDITORIAL CORRECTIONS — apply to this article]
- Field 'content': previously said "X", correct to "Y"
- Reason: factual_accuracy
```

This ensures the agent doesn't repeat mistakes that editors have already flagged. The correction fast-path writes within 1s of the editor action; the agent picks it up on the very next run for that market.

---

## Observability

Every pipeline run creates one OTel child span under the trace propagated from `topic.trending`. Key span attributes:

| Attribute | Set by |
|-----------|--------|
| `article_id` | pipeline entry |
| `market` | pipeline entry |
| `topic_id` | pipeline entry |
| `llm_provider_used` | `generate` node |
| `quality_score` | analytics RecordQualityScore call |
| `circuit_state_openai` | `check_circuit` |
| `circuit_state_anthropic` | `check_circuit` |
| `rate_limited` | `check_rate_limit` |
