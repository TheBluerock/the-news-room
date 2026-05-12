"""Agent service — consumes topic.trending, runs LangGraph pipeline, publishes article.generated."""

import logging
import os
import threading
from contextlib import asynccontextmanager

import grpc
import redis as redis_lib
import uvicorn
from anthropic import Anthropic
from confluent_kafka import Producer
from fastapi import FastAPI, Response
from openai import OpenAI

import consumer as consumer_mod
import telemetry
import vault as vaultpkg

logger = logging.getLogger("agent")
_ready = threading.Event()
_stop = threading.Event()


@asynccontextmanager
async def lifespan(app: FastAPI):
    try:
        secrets = vaultpkg.load("agent")
        openai_key = vaultpkg.require(secrets, "openai_api_key")
        anthropic_key = vaultpkg.require(secrets, "anthropic_api_key")
        redis_addr = vaultpkg.require(secrets, "redis_addr")
        redpanda_brokers = vaultpkg.require(secrets, "redpanda_brokers")
        learner_addr = os.getenv("LEARNER_ADDR", "learner:8080")
        learner_rest_url = "http://" + os.getenv("LEARNER_REST_ADDR", "learner:8088")
        analytics_addr = os.getenv("ANALYTICS_ADDR", "analytics:8080")
        logger.info("agent secrets loaded from Vault")
    except Exception as e:
        logger.error("vault load failed: %s", e)
        raise

    telemetry.configure("agent")
    logger.info("telemetry configured")

    rdb = redis_lib.Redis.from_url(f"redis://{redis_addr}", decode_responses=False)
    openai_client = OpenAI(api_key=openai_key)
    anthropic_client = Anthropic(api_key=anthropic_key)
    learner_channel = grpc.insecure_channel(learner_addr)
    analytics_channel = grpc.insecure_channel(analytics_addr)
    producer = Producer({"bootstrap.servers": redpanda_brokers, "acks": "all"})

    _stop.clear()
    t = threading.Thread(
        target=consumer_mod.run,
        args=(redpanda_brokers, rdb, learner_channel, learner_rest_url,
              analytics_channel, producer, openai_client, anthropic_client, _stop),
        daemon=True,
    )
    t.start()

    _ready.set()
    logger.info("agent service ready")
    yield

    _stop.set()
    _ready.clear()
    producer.flush(timeout=10)
    learner_channel.close()
    analytics_channel.close()
    rdb.close()


health_app = FastAPI(lifespan=lifespan, docs_url=None, redoc_url=None)


@health_app.get("/health")
def health():
    return {"status": "ok"}


@health_app.get("/ready")
def ready(response: Response):
    if not _ready.is_set():
        response.status_code = 503
        return {"status": "not ready"}
    return {"status": "ready"}


@health_app.get("/metrics")
def metrics():
    from prometheus_client import generate_latest, CONTENT_TYPE_LATEST
    return Response(content=generate_latest(), media_type=CONTENT_TYPE_LATEST)


if __name__ == "__main__":
    logging.basicConfig(level=logging.INFO, format="%(levelname)s %(name)s %(message)s")
    uvicorn.run(health_app, host="0.0.0.0", port=8090, log_config=None)
