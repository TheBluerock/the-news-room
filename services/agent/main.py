"""Agent service — consumes topic.trending, runs LangGraph pipeline, publishes article.generated."""

import asyncio
import threading
from contextlib import asynccontextmanager

import uvicorn
from fastapi import FastAPI, Response

_ready = threading.Event()


@asynccontextmanager
async def lifespan(app: FastAPI):
    # TODO: connect to Redis, RedPanda, Vault, gRPC channels to Learner + Analytics
    # TODO: initialise OTel tracer
    # TODO: start RedPanda consumer loop in background thread
    _ready.set()
    yield
    _ready.clear()


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


# Prometheus metrics — expose via /metrics
@health_app.get("/metrics")
def metrics():
    # TODO: wire prometheus_client exposition
    return Response(content="", media_type="text/plain")


if __name__ == "__main__":
    uvicorn.run(health_app, host="0.0.0.0", port=8090, log_config=None)
