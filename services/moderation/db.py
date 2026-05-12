"""PostgreSQL connection pool for the moderation service."""
import psycopg2
import psycopg2.pool
import psycopg2.extras
import threading

_pool: psycopg2.pool.ThreadedConnectionPool | None = None
_lock = threading.Lock()


def init(dsn: str, minconn: int = 1, maxconn: int = 5) -> None:
    global _pool
    with _lock:
        if _pool is None:
            _pool = psycopg2.pool.ThreadedConnectionPool(minconn, maxconn, dsn)


def get() -> psycopg2.pool.ThreadedConnectionPool:
    if _pool is None:
        raise RuntimeError("db not initialised — call db.init() first")
    return _pool


def execute(sql: str, params: tuple = ()) -> None:
    pool = get()
    conn = pool.getconn()
    try:
        with conn.cursor() as cur:
            cur.execute(sql, params)
        conn.commit()
    finally:
        pool.putconn(conn)


def fetchall(sql: str, params: tuple = ()) -> list[dict]:
    pool = get()
    conn = pool.getconn()
    try:
        with conn.cursor(cursor_factory=psycopg2.extras.RealDictCursor) as cur:
            cur.execute(sql, params)
            return [dict(r) for r in cur.fetchall()]
    finally:
        pool.putconn(conn)


def fetchone(sql: str, params: tuple = ()) -> dict | None:
    pool = get()
    conn = pool.getconn()
    try:
        with conn.cursor(cursor_factory=psycopg2.extras.RealDictCursor) as cur:
            cur.execute(sql, params)
            row = cur.fetchone()
            return dict(row) if row else None
    finally:
        pool.putconn(conn)
