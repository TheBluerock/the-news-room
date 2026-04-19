"""gRPC client for LearnerService.QueryKnowledgeGraph."""
import logging

import grpc

from proto import learner_pb2, learner_pb2_grpc

logger = logging.getLogger("agent.pipeline.context")


def fetch(learner_channel: grpc.Channel, market: str, topic_name: str, limit: int = 5) -> list[dict]:
    """Query Learner knowledge graph for context nodes."""
    stub = learner_pb2_grpc.LearnerServiceStub(learner_channel)
    try:
        resp = stub.QueryKnowledgeGraph(
            learner_pb2.QueryRequest(market=market, query=topic_name, limit=limit),
            timeout=5.0,
        )
        return [
            {"id": n.id, "type": n.type, "content": n.content, "weight": n.weight}
            for n in resp.nodes
        ]
    except grpc.RpcError as e:
        logger.warning("learner gRPC error: %s — using empty context", e)
        return []
