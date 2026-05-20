"""Integration tests for pipeline/circuit.py — uses real Redis via testcontainers."""
from __future__ import annotations

import time

import pytest

from pipeline import circuit


def _state_key(market, provider):
    return f"circuit:{market}:{provider}:state"


def test_initial_state_closed(redis_client):
    assert circuit.get_state(redis_client, "italy", "openai") == circuit.STATE_CLOSED
    assert circuit.is_available(redis_client, "italy", "openai") is True


def test_record_failure_increments_count(redis_client):
    circuit.record_failure(redis_client, "italy", "openai")
    count = int(redis_client.get("circuit:italy:openai:failures"))
    assert count == 1


def test_circuit_trips_at_threshold(redis_client):
    for _ in range(circuit.FAILURE_THRESHOLD - 1):
        circuit.record_failure(redis_client, "italy", "openai")
    # Not yet open
    assert circuit.get_state(redis_client, "italy", "openai") == circuit.STATE_CLOSED

    # The Nth failure trips it
    circuit.record_failure(redis_client, "italy", "openai")
    assert circuit.get_state(redis_client, "italy", "openai") == circuit.STATE_OPEN
    assert circuit.is_available(redis_client, "italy", "openai") is False


def test_record_success_resets_state(redis_client):
    # Trip the circuit
    for _ in range(circuit.FAILURE_THRESHOLD):
        circuit.record_failure(redis_client, "italy", "openai")
    assert circuit.get_state(redis_client, "italy", "openai") == circuit.STATE_OPEN

    # Success clears state + failure count
    circuit.record_success(redis_client, "italy", "openai")
    assert circuit.get_state(redis_client, "italy", "openai") == circuit.STATE_CLOSED
    assert redis_client.get("circuit:italy:openai:failures") is None


def test_half_open_after_delay(redis_client, monkeypatch):
    # Trip the circuit
    for _ in range(circuit.FAILURE_THRESHOLD):
        circuit.record_failure(redis_client, "italy", "openai")

    # Rewind opened_at so the half-open window has elapsed
    opened_at = time.time() - (circuit.HALF_OPEN_AFTER + 1)
    redis_client.set("circuit:italy:openai:opened_at", opened_at)

    state = circuit.get_state(redis_client, "italy", "openai")
    assert state == circuit.STATE_HALF_OPEN
    # Half-open allows traffic
    assert circuit.is_available(redis_client, "italy", "openai") is True


def test_half_open_before_delay_stays_open(redis_client):
    for _ in range(circuit.FAILURE_THRESHOLD):
        circuit.record_failure(redis_client, "italy", "openai")
    # opened_at is "now" — too soon to transition to half-open
    state = circuit.get_state(redis_client, "italy", "openai")
    assert state == circuit.STATE_OPEN
    assert circuit.is_available(redis_client, "italy", "openai") is False


def test_circuit_isolated_per_market(redis_client):
    for _ in range(circuit.FAILURE_THRESHOLD):
        circuit.record_failure(redis_client, "italy", "openai")
    assert circuit.get_state(redis_client, "italy", "openai") == circuit.STATE_OPEN
    # USA market untouched
    assert circuit.get_state(redis_client, "usa", "openai") == circuit.STATE_CLOSED


def test_circuit_isolated_per_provider(redis_client):
    for _ in range(circuit.FAILURE_THRESHOLD):
        circuit.record_failure(redis_client, "italy", "openai")
    assert circuit.get_state(redis_client, "italy", "openai") == circuit.STATE_OPEN
    # Anthropic for same market untouched
    assert circuit.get_state(redis_client, "italy", "anthropic") == circuit.STATE_CLOSED


def test_failure_counter_expires_outside_window(redis_client):
    # Failures expire after FAILURE_WINDOW seconds — we can't wait that long in tests,
    # so verify the TTL is set correctly.
    circuit.record_failure(redis_client, "italy", "openai")
    ttl = redis_client.ttl("circuit:italy:openai:failures")
    assert 0 < ttl <= circuit.FAILURE_WINDOW


@pytest.mark.parametrize("provider", ["openai", "anthropic"])
def test_both_providers_observe_threshold(redis_client, provider):
    for _ in range(circuit.FAILURE_THRESHOLD):
        circuit.record_failure(redis_client, "italy", provider)
    assert circuit.get_state(redis_client, "italy", provider) == circuit.STATE_OPEN
