"""Integration tests for db.py — uses real Postgres via testcontainers."""
from __future__ import annotations

import importlib
import sys

import pytest

# conftest stubs psycopg2 with MagicMock; force real psycopg2 here.
for mod in ("psycopg2", "psycopg2.pool", "psycopg2.extras"):
    sys.modules.pop(mod, None)
import psycopg2  # noqa: E402  pylint: disable=wrong-import-position

# Reload db.py against real psycopg2.
sys.modules.pop("db", None)
db = importlib.import_module("db")

try:
    from testcontainers.postgres import PostgresContainer
except ImportError:  # pragma: no cover
    PostgresContainer = None


@pytest.fixture(scope="module")
def pg_container():
    if PostgresContainer is None:
        pytest.skip("testcontainers unavailable")
    c = PostgresContainer("postgres:16-alpine")
    c.start()
    yield c
    c.stop()


@pytest.fixture()
def dsn(pg_container):
    return pg_container.get_connection_url().replace("postgresql+psycopg2://", "postgresql://")


@pytest.fixture(autouse=True)
def reset_pool():
    """db.py keeps a process-global pool — close & reset between tests so
    psycopg2 connections do not leak across the module-scoped container."""
    yield
    pool = getattr(db, "_pool", None)
    if pool is not None:
        closer = getattr(pool, "closeall", None) or getattr(pool, "close", None)
        if closer is not None:
            try:
                closer()
            except Exception:
                pass
    db._pool = None


def _seed_schema(dsn: str) -> None:
    conn = psycopg2.connect(dsn)
    try:
        with conn.cursor() as cur:
            cur.execute(
                "CREATE TABLE IF NOT EXISTS t (id SERIAL PRIMARY KEY, v TEXT)"
            )
        conn.commit()
    finally:
        conn.close()


def test_get_before_init_raises():
    with pytest.raises(RuntimeError, match="db not initialised"):
        db.get()


def test_init_creates_pool(dsn):
    db.init(dsn, minconn=1, maxconn=2)
    pool = db.get()
    assert pool is not None


def test_init_idempotent(dsn):
    db.init(dsn)
    first = db.get()
    db.init(dsn)  # second call must NOT replace pool
    assert db.get() is first


def test_execute_and_fetchall_roundtrip(dsn):
    _seed_schema(dsn)
    db.init(dsn)

    db.execute("INSERT INTO t (v) VALUES (%s)", ("alpha",))
    db.execute("INSERT INTO t (v) VALUES (%s)", ("beta",))
    rows = db.fetchall("SELECT v FROM t ORDER BY id")
    assert rows == [{"v": "alpha"}, {"v": "beta"}]


def test_fetchone_returns_dict_or_none(dsn):
    _seed_schema(dsn)
    db.init(dsn)

    db.execute("INSERT INTO t (v) VALUES (%s)", ("solo",))
    row = db.fetchone("SELECT v FROM t WHERE v = %s", ("solo",))
    assert row == {"v": "solo"}

    missing = db.fetchone("SELECT v FROM t WHERE v = %s", ("not-present",))
    assert missing is None


def test_fetchall_empty_returns_empty_list(dsn):
    _seed_schema(dsn)
    db.init(dsn)
    rows = db.fetchall("SELECT v FROM t WHERE v = %s", ("never",))
    assert rows == []


def test_execute_rollback_on_sql_error(dsn):
    _seed_schema(dsn)
    db.init(dsn)

    # Sanity baseline: row count before the failing statement.
    before = db.fetchone("SELECT count(*)::int AS n FROM t")["n"]

    # db.execute opens a single statement per connection and commit()s on
    # success; on SQL error the commit is skipped, the conn is returned to the
    # pool with its transaction discarded — so no rows from the failing call
    # should appear and no prior row should disappear.
    with pytest.raises(psycopg2.Error):
        db.execute("INSERT INTO nonexistent_table (x) VALUES (%s)", (1,))

    after = db.fetchone("SELECT count(*)::int AS n FROM t")["n"]
    assert after == before, "failed execute must not alter committed state"


def test_pool_releases_connections_under_load(dsn):
    """maxconn=2; do >2 sequential operations to verify conn return-to-pool works."""
    _seed_schema(dsn)
    db.init(dsn, minconn=1, maxconn=2)
    before = db.fetchone("SELECT count(*)::int AS n FROM t")["n"]
    for i in range(10):
        db.execute("INSERT INTO t (v) VALUES (%s)", (f"row-{i}",))
    rows = db.fetchall("SELECT count(*)::int AS n FROM t")
    assert rows[0]["n"] == before + 10
