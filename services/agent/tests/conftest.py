"""Shared fixtures for agent tests.

`redis_client` spins up a redis:7-alpine container per session and yields a
flushed client per test. Pytest skips integration tests if Docker is unavailable.
"""
from __future__ import annotations

import os
import sys
from pathlib import Path

import pytest

# Make `services/agent/` importable as the project root.
ROOT = Path(__file__).resolve().parents[1]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

# Generated proto stubs use root-level `import learner_pb2` — add proto/ to path.
PROTO = ROOT / "proto"
if PROTO.is_dir() and str(PROTO) not in sys.path:
    sys.path.insert(0, str(PROTO))

try:
    import redis as redis_lib
    from testcontainers.redis import RedisContainer
except ImportError as e:  # pragma: no cover
    redis_lib = None
    RedisContainer = None
    _IMPORT_ERR = e
else:
    _IMPORT_ERR = None


@pytest.fixture(scope="session")
def _redis_container():
    if RedisContainer is None:
        pytest.skip(f"testcontainers unavailable: {_IMPORT_ERR}")
    if os.getenv("SKIP_DOCKER_TESTS") == "1":
        pytest.skip("SKIP_DOCKER_TESTS=1")
    container = RedisContainer("redis:7-alpine")
    container.start()
    yield container
    container.stop()


@pytest.fixture()
def redis_client(_redis_container):
    """Clean Redis client per test (FLUSHDB before yield)."""
    host = _redis_container.get_container_host_ip()
    port = _redis_container.get_exposed_port(6379)
    client = redis_lib.Redis(host=host, port=int(port), decode_responses=False)
    client.flushdb()
    yield client
    client.close()
