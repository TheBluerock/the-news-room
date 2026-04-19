"""gRPC client for AnalyticsService quality recording."""
import logging

import grpc

from proto import analytics_pb2, analytics_pb2_grpc

logger = logging.getLogger("agent.pipeline.analytics")


def record_quality(analytics_channel: grpc.Channel, article_id: str, market: str, score: float) -> None:
    stub = analytics_pb2_grpc.AnalyticsServiceStub(analytics_channel)
    try:
        stub.RecordQualityScore(
            analytics_pb2.QualityRequest(article_id=article_id, market=market, quality_score=score),
            timeout=3.0,
        )
    except grpc.RpcError as e:
        logger.warning("analytics quality record failed: %s", e)
