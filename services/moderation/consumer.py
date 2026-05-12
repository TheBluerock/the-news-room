"""Moderation consumer: article.generated → LLM checks → article.approved / moderation.rejected."""
import json
import logging
import threading
import time
import uuid

import grpc
from confluent_kafka import Consumer, KafkaError, Producer
from openai import OpenAI
from opentelemetry import propagate, trace

import checks
import db as dbmod

logger = logging.getLogger("moderation.consumer")
tracer = trace.get_tracer("moderation.consumer")


def run(
    brokers: str,
    openai_client: OpenAI,
    producer: Producer,
    stop_event: threading.Event,
) -> None:
    consumer = Consumer({
        "bootstrap.servers": brokers,
        "group.id": "moderation-checker",
        "auto.offset.reset": "latest",
        "enable.auto.commit": False,
    })
    dlq_producer = Producer({"bootstrap.servers": brokers})

    consumer.subscribe(["article.generated"])
    logger.info("moderation consumer started — subscribed to article.generated")

    try:
        while not stop_event.is_set():
            msg = consumer.poll(timeout=1.0)
            if msg is None:
                continue
            if msg.error():
                if msg.error().code() != KafkaError._PARTITION_EOF:
                    logger.error("consumer error: %s", msg.error())
                continue

            try:
                event = json.loads(msg.value())
                _process(event, openai_client, producer, dlq_producer, brokers)
                consumer.commit(message=msg)
            except (json.JSONDecodeError, KeyError) as e:
                logger.error("bad message: %s", e)
                dlq_producer.produce("article.generated.dlq", value=msg.value())
                dlq_producer.poll(0)
                consumer.commit(message=msg)
    finally:
        consumer.close()
        dlq_producer.flush()


def _process(event: dict, openai_client: OpenAI, producer: Producer, dlq_producer: Producer, brokers: str) -> None:
    article_id = event["article_id"]
    market = event["market"]
    content = event["content"]
    trace_id = event.get("trace_id", "")

    # Extract trace context for distributed tracing
    carrier = {"traceparent": trace_id} if trace_id else {}
    ctx = propagate.extract(carrier)

    with tracer.start_as_current_span("moderation.check", context=ctx) as span:
        span.set_attribute("article_id", article_id)
        span.set_attribute("market", market)

        cultural_ok, cultural_reason = checks.check_cultural(content, market, openai_client)
        factual_ok, accuracy_score, issues = checks.check_factual(content, market, openai_client)
        quality = checks.score_quality(content)

        passed = cultural_ok and factual_ok and quality >= 0.5

        span.set_attribute("cultural_ok", cultural_ok)
        span.set_attribute("factual_ok", factual_ok)
        span.set_attribute("quality_score", quality)

        # Inject current trace into output headers
        out_carrier: dict = {}
        propagate.inject(out_carrier)
        headers = [(k, v.encode()) for k, v in out_carrier.items()]

        if passed:
            status = "auto_approved"
            _publish_approved(producer, event, quality, trace_id, headers)
        else:
            status = "auto_rejected"
            reason = cultural_reason if not cultural_ok else (", ".join(issues) or "quality too low")
            _publish_rejected(producer, article_id, market, reason, cultural_ok, factual_ok, quality, trace_id, headers)

        _save_to_queue(
            article_id=article_id,
            market=market,
            topic=event.get("topic_name", event.get("title", "")),
            status=status,
            quality=quality,
            cultural_ok=cultural_ok,
            factual_ok=factual_ok,
            rejection_reasons=issues if not passed else [],
        )


def _save_to_queue(
    article_id: str, market: str, topic: str, status: str,
    quality: float, cultural_ok: bool, factual_ok: bool, rejection_reasons: list,
) -> None:
    try:
        dbmod.execute(
            """INSERT INTO moderation_svc.review_queue
               (article_id, market, topic, status, quality_score, cultural_ok, factual_ok, rejection_reasons)
               VALUES (%s::uuid, %s, %s, %s, %s, %s, %s, %s)
               ON CONFLICT DO NOTHING""",
            (article_id, market, topic, status, quality, cultural_ok, factual_ok, rejection_reasons),
        )
    except Exception as e:
        logger.warning("failed to save to review_queue: %s", e)


def _publish_approved(producer, event, quality, trace_id, headers):
    approved = {
        "event_id":    str(uuid.uuid4()),
        "trace_id":    trace_id,
        "article_id":  event["article_id"],
        "market":      event["market"],
        "language":    event["language"],
        "content":     event["content"],
        "title":       event.get("title", ""),
        "excerpt":     event.get("excerpt", ""),
        "section":     event.get("section", "territori"),
        "author":      event.get("author", ""),
        "tags":        event.get("tags", []),
        "slug":        event.get("slug", ""),
        "moderator_id": "moderation-service",
        "quality_score": quality,
        "timestamp":   time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }
    producer.produce(
        "article.approved",
        key=event["article_id"].encode(),
        value=json.dumps(approved).encode(),
        headers=headers,
    )
    producer.poll(0)
    logger.info("article approved: %s market=%s quality=%.2f", event["article_id"], event["market"], quality)


def _publish_rejected(producer, article_id, market, reason, cultural_ok, factual_ok, quality, trace_id, headers):
    rejected = {
        "event_id": str(uuid.uuid4()),
        "trace_id": trace_id,
        "article_id": article_id,
        "market": market,
        "reason": reason,
        "checks": {
            "cultural_sensitivity": cultural_ok,
            "factual_accuracy": factual_ok,
            "quality_score": quality,
        },
        "timestamp": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }
    producer.produce(
        "moderation.rejected",
        key=article_id.encode(),
        value=json.dumps(rejected).encode(),
        headers=headers,
    )
    producer.poll(0)
    logger.warning("article rejected: %s market=%s reason=%s", article_id, market, reason)
