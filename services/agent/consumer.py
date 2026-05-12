"""RedPanda consumer for topic.trending — triggers the LangGraph pipeline.

When AGENT_MARKET env var is set, this instance only processes events for that
market (per-market deployment mode). Without it, all markets are handled
(single-process dev mode). Per-market mode uses group.id=agent-{market} so
Italy/USA/China instances each get all partitions independently.
"""
import json
import logging
import os
import threading
import time

import grpc
import redis as redis_lib
from confluent_kafka import Consumer, KafkaError, Producer

import pipeline as pipeline_mod

logger = logging.getLogger("agent.consumer")

# Per-market semaphore: 2 concurrent runs max per market
_market_semaphores: dict[str, threading.Semaphore] = {
    "italy": threading.Semaphore(2),
    "usa":   threading.Semaphore(2),
    "china": threading.Semaphore(2),
}

# If AGENT_MARKET is set, this instance only processes events for that market.
_AGENT_MARKET: str | None = os.getenv("AGENT_MARKET")

_graph = pipeline_mod.build_graph()


def run(
    brokers: str,
    rdb: redis_lib.Redis,
    learner_channel: grpc.Channel,
    learner_rest_url: str,
    analytics_channel: grpc.Channel,
    producer: Producer,
    openai_client,
    anthropic_client,
    stop_event: threading.Event,
) -> None:
    group_id = f"agent-{_AGENT_MARKET}" if _AGENT_MARKET else "agent-pipeline"
    consumer = Consumer({
        "bootstrap.servers": brokers,
        "group.id": group_id,
        "auto.offset.reset": "latest",
        "enable.auto.commit": False,
    })
    dlq_producer = Producer({"bootstrap.servers": brokers})

    consumer.subscribe(["topic.trending"])
    logger.info("agent consumer started — subscribed to topic.trending group=%s market_filter=%s",
                group_id, _AGENT_MARKET or "all")

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
                market = event.get("market", "")
                topic_id = event.get("topic_id", "")
                topic_name = event.get("topic_name", "")
                trace_id = event.get("trace_id", "")

                if market not in _market_semaphores:
                    logger.warning("unknown market: %s", market)
                    consumer.commit(message=msg)
                    continue

                # Per-market instance: skip events for other markets
                if _AGENT_MARKET and market != _AGENT_MARKET:
                    consumer.commit(message=msg)
                    continue

                sem = _market_semaphores[market]
                if not sem.acquire(blocking=False):
                    logger.warning("market=%s at max concurrency, skipping topic=%s", market, topic_name)
                    consumer.commit(message=msg)
                    continue

                # Run pipeline in background thread
                t = threading.Thread(
                    target=_run_pipeline,
                    args=(sem, rdb, learner_channel, learner_rest_url,
                          analytics_channel, producer,
                          openai_client, anthropic_client,
                          market, topic_id, topic_name, trace_id),
                    daemon=True,
                )
                t.start()
                consumer.commit(message=msg)

            except (json.JSONDecodeError, KeyError) as e:
                logger.error("bad message: %s", e)
                _send_to_dlq(dlq_producer, msg.value())
                consumer.commit(message=msg)
    finally:
        consumer.close()
        dlq_producer.flush()


def _run_pipeline(sem, rdb, learner_channel, learner_rest_url,
                  analytics_channel, producer,
                  openai_client, anthropic_client,
                  market, topic_id, topic_name, trace_id):
    try:
        state = pipeline_mod.ArticleState(
            market=market,
            topic_id=topic_id,
            topic_name=topic_name,
            trace_id=trace_id,
            rdb=rdb,
            learner_channel=learner_channel,
            learner_rest_url=learner_rest_url,
            analytics_channel=analytics_channel,
            producer=producer,
            openai_client=openai_client,
            anthropic_client=anthropic_client,
            memory={},
            corrections_data={},
            context=[],
            quality_summary={},
            prompt="",
            content="",
            article_id="",
        )
        _graph.invoke(state)
        logger.info("pipeline completed market=%s topic=%s", market, topic_name)
    except Exception as e:
        logger.error("pipeline failed market=%s topic=%s: %s", market, topic_name, e, exc_info=True)
    finally:
        sem.release()


def _send_to_dlq(producer: Producer, value: bytes) -> None:
    try:
        producer.produce("topic.trending.dlq", value=value)
        producer.poll(0)
    except Exception as e:
        logger.error("dlq produce failed: %s", e)
