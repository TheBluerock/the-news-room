"""Integration tests for services/moderation/api.py.

Uses FastAPI TestClient (synchronous ASGI transport — no real HTTP server needed).
All DB calls are mocked via unittest.mock.patch so no PostgreSQL required.
Kafka producer on app.state is also mocked.
"""
import datetime
import uuid
from unittest.mock import MagicMock, patch

import pytest
from fastapi.testclient import TestClient

import api as api_mod

ADMIN = {"X-User-Role": "admin"}
NON_ADMIN = {"X-User-Role": "viewer"}

# ── fixtures ──────────────────────────────────────────────────────────────────


@pytest.fixture()
def client():
    """TestClient with a mock Kafka producer attached to app.state."""
    producer = MagicMock()
    api_mod.app.state.producer = producer
    with TestClient(api_mod.app, raise_server_exceptions=True) as c:
        c._producer = producer
        yield c


# ── GET /api/moderation/queue ─────────────────────────────────────────────────


def test_queue_requires_admin(client):
    resp = client.get("/api/moderation/queue", headers=NON_ADMIN)
    assert resp.status_code == 403


def test_queue_empty(client):
    with patch("api.dbmod.fetchall", return_value=[]) as mock_fetch:
        resp = client.get("/api/moderation/queue", headers=ADMIN)
    assert resp.status_code == 200
    assert resp.json() == []
    mock_fetch.assert_called_once()


def test_queue_returns_items(client):
    article_id = str(uuid.uuid4())
    item_id = str(uuid.uuid4())
    fake_row = {
        "id": uuid.UUID(item_id),
        "article_id": uuid.UUID(article_id),
        "market": "italy",
        "topic": "Barolo 2023",
        "status": "auto_rejected",
        "quality_score": 0.45,
        "cultural_ok": True,
        "factual_ok": False,
        "rejection_reasons": ["factual_inaccuracy"],
        "created_at": datetime.datetime(2026, 5, 1, 12, 0, 0),
    }
    with patch("api.dbmod.fetchall", return_value=[fake_row]):
        resp = client.get(
            "/api/moderation/queue",
            params={"market": "italy", "status": "auto_rejected"},
            headers=ADMIN,
        )
    assert resp.status_code == 200
    data = resp.json()
    assert len(data) == 1
    item = data[0]
    assert item["market"] == "italy"
    assert item["score"] == pytest.approx(0.45)
    assert item["rejection_reasons"] == ["factual_inaccuracy"]


def test_queue_market_filter_passed_to_db(client):
    with patch("api.dbmod.fetchall", return_value=[]) as mock_fetch:
        client.get(
            "/api/moderation/queue",
            params={"market": "china"},
            headers=ADMIN,
        )
    call_args = mock_fetch.call_args
    sql, params = call_args[0]
    assert "china" in params


# ── POST /api/moderation/approve/{item_id} ────────────────────────────────────


def test_approve_requires_admin(client):
    with patch("api.dbmod.fetchone", return_value=None):
        resp = client.post(f"/api/moderation/approve/{uuid.uuid4()}", headers=NON_ADMIN)
    assert resp.status_code == 403


def test_approve_not_found(client):
    with patch("api.dbmod.fetchone", return_value=None):
        resp = client.post(f"/api/moderation/approve/{uuid.uuid4()}", headers=ADMIN)
    assert resp.status_code == 404


def test_approve_ok_emits_kafka(client):
    article_id = str(uuid.uuid4())
    item_id = str(uuid.uuid4())
    row = {"article_id": uuid.UUID(article_id), "market": "italy"}

    with patch("api.dbmod.fetchone", return_value=row), \
         patch("api.dbmod.execute") as mock_exec:
        resp = client.post(
            f"/api/moderation/approve/{item_id}",
            headers={**ADMIN, "X-User-ID": "editor-007"},
        )

    assert resp.status_code == 200
    assert resp.json() == {"approved": True}

    # DB updated with human_approved
    mock_exec.assert_called_once()
    exec_sql = mock_exec.call_args[0][0]
    assert "human_approved" in exec_sql

    # Kafka produce called
    client._producer.produce.assert_called_once()
    kwargs = client._producer.produce.call_args[1]
    assert kwargs["key"] == article_id.encode()
    import json
    payload = json.loads(kwargs["value"].decode())
    assert payload["market"] == "italy"
    assert payload["quality_score"] == pytest.approx(1.0)


def test_approve_ok_no_producer(client):
    """Approve succeeds even when producer not set (no crash)."""
    article_id = str(uuid.uuid4())
    item_id = str(uuid.uuid4())
    row = {"article_id": uuid.UUID(article_id), "market": "usa"}
    api_mod.app.state.producer = None

    with patch("api.dbmod.fetchone", return_value=row), \
         patch("api.dbmod.execute"):
        resp = client.post(f"/api/moderation/approve/{item_id}", headers=ADMIN)

    assert resp.status_code == 200
    api_mod.app.state.producer = client._producer  # restore


# ── POST /api/moderation/reject/{item_id} ────────────────────────────────────


def test_reject_requires_admin(client):
    resp = client.post(f"/api/moderation/reject/{uuid.uuid4()}", json={}, headers=NON_ADMIN)
    assert resp.status_code == 403


def test_reject_not_found(client):
    with patch("api.dbmod.fetchone", return_value=None):
        resp = client.post(
            f"/api/moderation/reject/{uuid.uuid4()}",
            json={"reason": "off-topic"},
            headers=ADMIN,
        )
    assert resp.status_code == 404


def test_reject_ok(client):
    item_id = str(uuid.uuid4())
    row = {"id": uuid.UUID(item_id)}

    with patch("api.dbmod.fetchone", return_value=row), \
         patch("api.dbmod.execute") as mock_exec:
        resp = client.post(
            f"/api/moderation/reject/{item_id}",
            json={"reason": "cultural_insensitivity"},
            headers=ADMIN,
        )

    assert resp.status_code == 200
    assert resp.json() == {"rejected": True}

    exec_sql = mock_exec.call_args[0][0]
    assert "human_rejected" in exec_sql
    exec_params = mock_exec.call_args[0][1]
    assert "cultural_insensitivity" in exec_params


def test_reject_default_reason(client):
    """Empty reason body gets default 'editor override' string."""
    item_id = str(uuid.uuid4())
    row = {"id": uuid.UUID(item_id)}

    with patch("api.dbmod.fetchone", return_value=row), \
         patch("api.dbmod.execute") as mock_exec:
        resp = client.post(
            f"/api/moderation/reject/{item_id}",
            json={},
            headers=ADMIN,
        )

    assert resp.status_code == 200
    exec_params = mock_exec.call_args[0][1]
    assert "editor override" in exec_params
