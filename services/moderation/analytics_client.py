"""gRPC client for AnalyticsService.RecordQualityScore."""
import logging
import sys
import os

import grpc

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "proto"))
from proto import analytics_pb2, analytics_pb2_grpc  # noqa: E402

logger = logging.getLogger("moderation.analytics_client")


def record_quality(
    analytics_channel: grpc.Channel,
    article_id: str,
    market: str,
    score: float,
    usage: dict | None = None,
) -> None:
    """Write quality score to analytics_svc.article_performance via gRPC.

    ``usage`` (optional) carries per-call LLM token data from the agent:
        {prompt_tokens, completion_tokens, model, cost_usd?}
    When present, analytics computes + persists USD cost. Non-fatal on error —
    moderation must NOT fail because analytics is unreachable (Phase K1).
    """
    stub = analytics_pb2_grpc.AnalyticsServiceStub(analytics_channel)
    usage = usage or {}
    try:
        stub.RecordQualityScore(
            analytics_pb2.QualityRequest(
                article_id=article_id,
                market=market,
                quality_score=score,
                prompt_tokens=int(usage.get("prompt_tokens") or 0),
                completion_tokens=int(usage.get("completion_tokens") or 0),
                model=str(usage.get("model") or ""),
            ),
            timeout=3.0,
        )
        logger.debug("quality recorded article_id=%s score=%.2f", article_id, score)
    except Exception as e:
        logger.warning("RecordQualityScore failed article_id=%s: %s", article_id, e)
