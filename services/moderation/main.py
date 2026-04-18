"""Moderation service — consumes article.generated, runs cultural + factual checks."""

import threading
from contextlib import asynccontextmanager

import uvicorn
from fastapi import FastAPI, Response

_ready = threading.Event()


@asynccontextmanager
async def lifespan(app: FastAPI):
    # TODO: connect to RedPanda, Vault, gRPC channel to Learner
    # TODO: initialise OTel tracer
    # TODO: start RedPanda consumer loop for article.generated
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


@health_app.get("/metrics")
def metrics():
    # TODO: wire prometheus_client exposition
    return Response(content="", media_type="text/plain")


# ── Moderation pipeline (stub) ────────────────────────────────────────────────

def check_cultural_sensitivity(market: str, content: str) -> tuple[bool, list[str]]:
    """Returns (passed, issues). Uses LLM with market-specific prompt."""
    # TODO: call OpenAI/Anthropic with cultural sensitivity prompt per market
    return True, []


def check_factual_accuracy(market: str, content: str) -> tuple[float, list[str]]:
    """Returns (accuracy_score, issues). Calls gRPC LearnerService.ScoreFactualAccuracy."""
    # TODO: gRPC call to Learner
    return 1.0, []


def score_quality(content: str) -> float:
    """LLM-based quality score: coherence, tone, market fit. Returns 0.0–1.0."""
    # TODO: call LLM
    return 1.0


def process_article(event: dict) -> None:
    """Main moderation logic for a single article.generated event."""
    market = event["market"]
    content = event["content"]
    article_id = event["article_id"]

    cultural_ok, cultural_issues = check_cultural_sensitivity(market, content)
    accuracy_score, factual_issues = check_factual_accuracy(market, content)
    quality = score_quality(content)

    # TODO: write immutable audit_log entry
    # TODO: if approved → publish article.approved with article_id as idempotency key
    # TODO: if rejected → publish moderation.rejected → triggers both correction paths
    # TODO: on any failure → publish to article.generated.dlq


if __name__ == "__main__":
    uvicorn.run(health_app, host="0.0.0.0", port=8090, log_config=None)
