# 03 — Event Flow

## Topic Registry

| Topic | Producer | Consumer(s) | Schema | Partitions |
|-------|----------|-------------|--------|-----------|
| `topic.trending` | analytics (calendar publisher) | agent | JSON | 3 |
| `article.generated` | agent | moderation | JSON | 3 |
| `article.approved` | moderation | Sanity connector (Phase 4) — **no consumer in Phase 3** | JSON | 3 |
| `article.published` | Sanity webhook / connector | analytics | JSON | 3 |
| `editor.correction` | frontend | correction, learner | JSON | 3 |
| `moderation.rejected` | moderation | correction, learner | JSON | 3 |
| `user.data.deletion.requested` | frontend (GDPR) | all services | JSON | 1 |

> **Phase 3 gap:** `article.approved` has no consumer until the Sanity connector is built in Phase 4. Approved articles accumulate in RedPanda and are not visible to editors. See `09-open-issues.md` PHASE4-01.

> **`user.data.deletion.requested` ordering:** This topic uses 1 partition with `user_id` as the Kafka message key. This guarantees that multiple deletion requests for the same user are processed in order. A second request arriving after the first will find the user already soft-deleted and is a safe no-op.

Every topic has a corresponding `.dlq` topic (e.g. `article.generated.dlq`). DLQ topics use the same partition count as the source.

All schemas are registered in the RedPanda Schema Registry at `http://redpanda:8081`. Schema compatibility is checked on every PR touching `infra/schemas/`.

---

## Full Event Chain

```
[Analytics — calendar publisher, ticks every 1min]
    reads analytics_svc.editorial_calendar WHERE scheduled_at <= now() AND dispatched = false
    topic.trending  {topic_id, topic_name, market, score, trace_id}
        │
        ▼
[Agent — LangGraph pipeline]
    article.generated  {article_id, market, language, content, trace_id, ...}
        │
        ▼
[Moderation — checks]
    ├── article.approved  {article_id, market, quality_score, moderator_id, trace_id, ...}
    │       │
    │       ▼
    │   [Sanity CMS — Phase 4]
    │       article.published  {article_id, market, sanity_id, trace_id, ...}
    │               │
    │               ▼
    │           [Analytics tracker]
    │               updates trending sorted set + quality DB
    │
    └── moderation.rejected  {article_id, market, reason, checks, trace_id}
            │
            ├── [Correction service — fast path]
            │       writes Redis  corrections:<market>:<correction_id>  48h TTL
            │
            └── [Learner — slow path]
                    updates PostgreSQL knowledge graph (1–6h)
                    regenerates HNSW embeddings
                    DELs Redis correction key after apply

[Frontend — editorial]
    editor.correction  {correction_id, market, article_id, field, old_value, new_value}
            │
            ├── [Correction service — fast path]  (same as above)
            └── [Learner — slow path]              (same as above)
```

---

## Event Schemas

### topic.trending
```json
{
  "event_id": "uuid",
  "trace_id": "W3C traceparent string",
  "topic_id": "string",
  "topic_name": "string",
  "market": "italy | usa | china",
  "score": "float",
  "timestamp": "ISO 8601 UTC"
}
```

### article.generated
```json
{
  "event_id": "uuid",
  "trace_id": "W3C traceparent string",
  "article_id": "uuid",
  "market": "italy | usa | china",
  "language": "it | en | zh",
  "content": "string (full article text)",
  "topic_id": "string",
  "topic_name": "string",
  "timestamp": "ISO 8601 UTC"
}
```

### article.approved
```json
{
  "event_id": "uuid",
  "trace_id": "W3C traceparent string",
  "article_id": "uuid",
  "market": "italy | usa | china",
  "language": "it | en | zh",
  "content": "string",
  "moderator_id": "moderation-service",
  "quality_score": "float (0.0–1.0)",
  "timestamp": "ISO 8601 UTC"
}
```

### moderation.rejected
```json
{
  "event_id": "uuid",
  "trace_id": "W3C traceparent string",
  "article_id": "uuid",
  "market": "italy | usa | china",
  "reason": "string",
  "checks": {
    "cultural_sensitivity": "bool",
    "factual_accuracy": "bool",
    "quality_score": "float"
  },
  "timestamp": "ISO 8601 UTC"
}
```

### editor.correction
```json
{
  "event_id": "uuid",
  "trace_id": "W3C traceparent string",
  "correction_id": "uuid",
  "article_id": "uuid",
  "market": "italy | usa | china",
  "field": "string (e.g. 'content', 'title')",
  "old_value": "string",
  "new_value": "string",
  "editor_id": "uuid",
  "timestamp": "ISO 8601 UTC"
}
```

---

## Idempotency Per Consumer

DLQ replay and at-least-once delivery mean every consumer can receive the same message more than once. Each consumer's idempotency strategy:

| Consumer | Topic | Strategy |
|----------|-------|----------|
| moderation | `article.generated` | No state written before producing output — re-processing produces a new `event_id` but the same `article_id`. Moderation is deterministic for the same content; duplicate approved/rejected events are handled by downstream consumers. |
| Sanity connector (Phase 4) | `article.approved` | `article_id` used as Sanity document `_id`. Sanity upsert is idempotent — replaying the same approval overwrites the same document with identical content. |
| analytics tracker | `article.published` | `UPSERT INTO analytics_svc.article_performance ... ON CONFLICT (article_id) DO UPDATE` — second delivery overwrites with same values, net effect is identical. |
| correction (fast path) | `editor.correction`, `moderation.rejected` | `SET corrections:<market>:<correction_id> ... NX` — `NX` flag means only the first delivery writes; subsequent deliveries are no-ops. |
| learner (slow path) | `editor.correction`, `moderation.rejected` | `INSERT ... ON CONFLICT (correction_id) DO UPDATE` — idempotent upsert. |
| all services | `user.data.deletion.requested` | Soft-delete: `UPDATE users SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`. Subsequent deliveries match zero rows — safe no-op. |

---

## DLQ Strategy

**Retry backoff per consumer:** 5s → 30s → 2m → DLQ (infrastructure failures: Redis unavailable, DB down, parse error)

**Note:** The agent pipeline uses a separate, slower backoff (60s → 5m → 30m → DLQ) for LLM-specific failures (circuit open, rate limit exhausted). These are intentionally different: consumer backoff handles transient infrastructure errors and resolves in seconds; pipeline backoff handles LLM budget/availability pressure that resolves in minutes.

**DLQ naming:** `<original-topic>.dlq` (e.g. `article.generated.dlq`)

**Alert:** DLQ depth > 0 triggers immediate Grafana alert (PagerDuty in prod).

**Inspection:** `make dlq-list` — shows all DLQ topics and pending message counts.

**Replay:** `make dlq-replay TOPIC=article.generated.dlq` — replays messages to the original topic.

**Runbook:** `ops/DLQ-handling.md`

> Never DELETE from a DLQ without inspecting the message first. Every DLQ message represents a real failure. Replay before discard.

---

## W3C TraceContext Propagation

Every event producer injects a `traceparent` header (W3C format) into Kafka message headers using the OTel propagator:

**Go (franz-go):**
```go
outCarrier := make(map[string]string)
propagate.Inject(ctx, propagation.MapCarrier(outCarrier))
headers := make([]kgo.RecordHeader, 0, len(outCarrier))
for k, v := range outCarrier {
    headers = append(headers, kgo.RecordHeader{Key: k, Value: []byte(v)})
}
```

**Python (confluent-kafka):**
```python
out_carrier: dict = {}
propagate.inject(out_carrier)
headers = [(k, v.encode()) for k, v in out_carrier.items()]
producer.produce(topic, ..., headers=headers)
```

Every consumer extracts this header and continues the trace:
```python
carrier = {"traceparent": trace_id} if trace_id else {}
ctx = propagate.extract(carrier)
with tracer.start_as_current_span("...", context=ctx) as span:
    ...
```

The result is a single OTel trace ID covering the full pipeline from `topic.trending` to `article.published` — visible in Grafana Tempo.

---

## Schema Registry

Schemas are JSON Schema files in `infra/schemas/`. They are registered at RedPanda startup via `infra/scripts/setup-redpanda.sh`.

**Compatibility mode:** BACKWARD — new schema versions must be readable by old consumers.

**Breaking changes** require a new event version (e.g. `article.generated.v2`) and a migration period where both versions are consumed simultaneously. Schema compatibility is checked on every PR touching `infra/schemas/` via `buf lint` equivalent.
