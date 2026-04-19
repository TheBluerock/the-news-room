"""Validates and publishes article.generated events to Redpanda."""
import json
import logging
import time
import uuid

import jsonschema
from confluent_kafka import Producer
from opentelemetry import propagate, trace

logger = logging.getLogger("agent.pipeline.publisher")

MARKET_LANGUAGE = {"italy": "it", "usa": "en", "china": "zh"}
_schema_cache: dict = {}


def _load_schema() -> dict:
    if "article.generated" not in _schema_cache:
        import pathlib
        schema_path = pathlib.Path(__file__).parents[3] / "infra" / "schemas" / "article.generated.json"
        with open(schema_path) as f:
            _schema_cache["article.generated"] = json.load(f)
    return _schema_cache["article.generated"]


def publish(
    producer: Producer,
    market: str,
    topic_id: str,
    article_id: str,
    content: str,
    trace_id: str,
) -> None:
    """Validate and produce article.generated to Redpanda."""
    event = {
        "event_id": str(uuid.uuid4()),
        "trace_id": trace_id,
        "article_id": article_id,
        "market": market,
        "language": MARKET_LANGUAGE.get(market, "en"),
        "content": content,
        "topic_id": topic_id,
        "quality_score": None,
        "timestamp": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }
    # Remove None values to satisfy schema (quality_score is optional)
    event = {k: v for k, v in event.items() if v is not None}

    schema = _load_schema()
    try:
        jsonschema.validate(event, schema)
    except jsonschema.ValidationError as e:
        logger.error("article.generated schema validation failed: %s", e.message)
        raise

    # Inject W3C TraceContext into Kafka headers
    carrier = {}
    propagate.inject(carrier)
    headers = [(k, v.encode()) for k, v in carrier.items()]

    producer.produce(
        "article.generated",
        key=article_id.encode(),
        value=json.dumps(event).encode(),
        headers=headers,
    )
    producer.poll(0)
    logger.info("article.generated published", extra={"article_id": article_id, "market": market})
