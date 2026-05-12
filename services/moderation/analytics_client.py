"""gRPC client for AnalyticsService.RecordQualityScore."""
import logging
import sys
import os

import grpc

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "proto"))
from proto import analytics_pb2, analytics_pb2_grpc  # noqa: E402

logger = logging.getLogger("moderation.analytics_client")


def record_quality(analytics_channel: grpc.Channel, article_id: str, market: str, score: float) -> None:
    """Write quality score to analytics_svc.article_performance via gRPC. Non-fatal on error."""
    stub = analytics_pb2_grpc.AnalyticsServiceStub(analytics_channel)
    try:
        stub.RecordQualityScore(
            analytics_pb2.QualityRequest(
                article_id=article_id,
                market=market,
                quality_score=score,
            ),
            timeout=3.0,
        )
        logger.debug("quality recorded article_id=%s score=%.2f", article_id, score)
    except Exception as e:
        logger.warning("RecordQualityScore failed article_id=%s: %s", article_id, e)
