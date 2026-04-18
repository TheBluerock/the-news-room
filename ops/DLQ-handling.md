# DLQ Handling Runbook

## When DLQ alert fires

A Grafana alert fires when any `*.dlq` topic has depth > 0.

## 1. Identify the affected topic

```bash
make dlq-list
```

## 2. Inspect messages

```bash
make dlq-replay TOPIC=article.generated.dlq  # preview — does NOT commit
# or directly:
go run ./cmd/dlq-tool inspect --topic article.generated.dlq --limit 20
```

## 3. Determine cause

Common causes:
- **Schema mismatch** — producer published event that fails schema validation. Fix: update producer, then replay.
- **Consumer panic** — service crashed processing a malformed message. Fix: deploy fix, then replay.
- **Dependency outage** — downstream service (Learner gRPC, PostgreSQL) was down. Fix: confirm recovery, then replay.
- **LLM circuit open** — both OpenAI and Anthropic circuits tripped. Fix: verify API keys in Vault, reset circuit, replay.

## 4. Replay

```bash
go run ./cmd/dlq-tool replay --topic article.generated.dlq
# replay specific count:
go run ./cmd/dlq-tool replay --topic article.generated.dlq --limit 10
```

## 5. Discard (last resort)

Only discard if messages are unrecoverable (e.g. stale test events, invalid data with no fix path).

```bash
go run ./cmd/dlq-tool discard --topic article.generated.dlq
# requires interactive confirmation
```

## DLQ topic map

| Original topic        | DLQ topic                   | Owner        |
|-----------------------|-----------------------------|--------------|
| topic.trending        | topic.trending.dlq          | Agent        |
| article.generated     | article.generated.dlq       | Moderation   |
| article.approved      | article.approved.dlq        | Sanity       |
| article.published     | article.published.dlq       | Analytics    |
| editor.correction     | editor.correction.dlq       | Correction + Learner |
| moderation.rejected   | moderation.rejected.dlq     | Correction + Learner |
