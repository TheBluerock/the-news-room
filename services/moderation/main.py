"""Moderation service — consumes article.generated, runs cultural + factual checks."""

import logging
import threading
from contextlib import asynccontextmanager

import uvicorn
from confluent_kafka import Producer
from fastapi import FastAPI, Response
from openai import OpenAI

import consumer as consumer_mod
import telemetry
import vault as vaultpkg

logger = logging.getLogger("moderation")
_ready = threading.Event()
_stop = threading.Event()


@asynccontextmanager
async def lifespan(app: FastAPI):
    try:
        secrets = vaultpkg.load("moderation")
        openai_key = vaultpkg.require(secrets, "openai_api_key")
        redpanda_brokers = vaultpkg.require(secrets, "redpanda_brokers")
        logger.info("moderation secrets loaded from Vault")
    except Exception as e:
        logger.error("vault load failed: %s", e)
        raise

    telemetry.configure("moderation")
    logger.info("telemetry configured")

    openai_client = OpenAI(api_key=openai_key)
    producer = Producer({"bootstrap.servers": redpanda_brokers, "acks": "all"})

    _stop.clear()
    t = threading.Thread(
        target=consumer_mod.run,
        args=(redpanda_brokers, openai_client, producer, _stop),
        daemon=True,
    )
    t.start()

    _ready.set()
    logger.info("moderation service ready")
    yield

    _stop.set()
    _ready.clear()
    producer.flush(timeout=10)


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
