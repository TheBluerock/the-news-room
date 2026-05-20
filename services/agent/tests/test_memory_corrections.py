"""Integration tests for pipeline/memory.py + pipeline/corrections.py."""
from __future__ import annotations

import json
import time

from pipeline import memory, corrections


# ── memory ──────────────────────────────────────────────────────────────────

def test_memory_fetch_empty(redis_client):
    assert memory.fetch(redis_client, "italy") == {}


def test_memory_write_then_fetch(redis_client):
    memory.write(redis_client, "italy", "Barolo 2018", "Vintage notes summary")
    data = memory.fetch(redis_client, "italy")
    assert "Barolo 2018" in data
    entry = data["Barolo 2018"]
    assert entry["topic_name"] == "Barolo 2018"
    assert entry["summary"] == "Vintage notes summary"
    assert "updated_at" in entry


def test_memory_write_truncates_summary_to_200(redis_client):
    long_summary = "x" * 500
    memory.write(redis_client, "italy", "topic", long_summary)
    data = memory.fetch(redis_client, "italy")
    assert len(data["topic"]["summary"]) == 200


def test_memory_write_sets_ttl(redis_client):
    memory.write(redis_client, "italy", "topic", "summary")
    ttl = redis_client.ttl("memory:italy")
    assert 0 < ttl <= memory.MEMORY_TTL


def test_memory_isolated_per_market(redis_client):
    memory.write(redis_client, "italy", "t1", "italy summary")
    memory.write(redis_client, "usa",   "t2", "usa summary")
    italy = memory.fetch(redis_client, "italy")
    usa = memory.fetch(redis_client, "usa")
    assert "t1" in italy and "t2" not in italy
    assert "t2" in usa and "t1" not in usa


def test_memory_overwrites_same_topic(redis_client):
    memory.write(redis_client, "italy", "topic", "first")
    memory.write(redis_client, "italy", "topic", "second")
    data = memory.fetch(redis_client, "italy")
    assert data["topic"]["summary"] == "second"


# ── corrections ─────────────────────────────────────────────────────────────

def test_corrections_fetch_empty(redis_client):
    assert corrections.fetch(redis_client, "italy") == {}


def test_corrections_fetch_returns_payloads(redis_client):
    redis_client.set("corrections:italy:c1", json.dumps({"reason": "use formal tone"}))
    redis_client.set("corrections:italy:c2", json.dumps({"reason": "no 2020 vintage"}))
    out = corrections.fetch(redis_client, "italy")
    assert set(out.keys()) == {"c1", "c2"}
    assert out["c1"]["reason"] == "use formal tone"
    assert out["c2"]["reason"] == "no 2020 vintage"


def test_corrections_isolated_per_market(redis_client):
    redis_client.set("corrections:italy:c1", json.dumps({"reason": "italy-only"}))
    redis_client.set("corrections:usa:cX",   json.dumps({"reason": "usa-only"}))
    italy = corrections.fetch(redis_client, "italy")
    usa = corrections.fetch(redis_client, "usa")
    assert "c1" in italy and "cX" not in italy
    assert "cX" in usa and "c1" not in usa


def test_corrections_skips_malformed_json(redis_client):
    redis_client.set("corrections:italy:good", json.dumps({"reason": "ok"}))
    redis_client.set("corrections:italy:bad",  b"{not valid json")
    out = corrections.fetch(redis_client, "italy")
    assert "good" in out
    assert "bad" not in out


def test_corrections_respects_ttl(redis_client):
    # Mimic the 48h TTL admin UI sets — we use 1s here.
    redis_client.set("corrections:italy:c1", json.dumps({"reason": "x"}), ex=1)
    assert "c1" in corrections.fetch(redis_client, "italy")
    time.sleep(1.5)
    assert corrections.fetch(redis_client, "italy") == {}
