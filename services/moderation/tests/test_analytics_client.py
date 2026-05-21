"""Unit tests for analytics_client.py — mocks gRPC stub, no network."""
from __future__ import annotations

import importlib
import sys
from unittest.mock import MagicMock

# conftest stubs analytics_client + grpc — force real imports here.
for mod in ("analytics_client", "grpc"):
    sys.modules.pop(mod, None)

import analytics_client  # noqa: E402
import grpc  # noqa: E402


def test_record_quality_calls_stub_with_correct_args(monkeypatch):
    mock_stub = MagicMock()
    monkeypatch.setattr(
        analytics_client.analytics_pb2_grpc, "AnalyticsServiceStub",
        lambda channel: mock_stub,
    )

    analytics_client.record_quality(
        analytics_channel=MagicMock(),
        article_id="art-1",
        market="italy",
        score=0.87,
    )

    mock_stub.RecordQualityScore.assert_called_once()
    call = mock_stub.RecordQualityScore.call_args
    req = call.args[0]
    assert req.article_id == "art-1"
    assert req.market == "italy"
    assert abs(req.quality_score - 0.87) < 1e-6
    assert call.kwargs["timeout"] == 3.0


def test_record_quality_swallows_grpc_errors(monkeypatch):
    """Defensive: gRPC failure must NOT raise — non-fatal contract."""
    mock_stub = MagicMock()
    # Use a real grpc.RpcError subclass so the swallow path is exercised against
    # the same exception type the analytics stub actually raises in prod, not a
    # generic RuntimeError.
    class _FakeRpcError(grpc.RpcError):
        pass
    mock_stub.RecordQualityScore.side_effect = _FakeRpcError("analytics unreachable")
    monkeypatch.setattr(
        analytics_client.analytics_pb2_grpc, "AnalyticsServiceStub",
        lambda channel: mock_stub,
    )

    # Must NOT raise.
    analytics_client.record_quality(MagicMock(), "art-2", "italy", 0.5)


def test_record_quality_score_boundaries(monkeypatch):
    mock_stub = MagicMock()
    monkeypatch.setattr(
        analytics_client.analytics_pb2_grpc, "AnalyticsServiceStub",
        lambda channel: mock_stub,
    )

    for score in (0.0, 0.5, 1.0):
        analytics_client.record_quality(MagicMock(), "art", "italy", score)
    assert mock_stub.RecordQualityScore.call_count == 3
