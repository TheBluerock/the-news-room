"""Stub out psycopg2 and confluent_kafka before any test module imports api.py."""
import sys
from types import ModuleType
from unittest.mock import MagicMock

# psycopg2 is not installed in the test environment (no pg_config).
# db.py only uses it at runtime; tests mock all db.* calls directly.
if "psycopg2" not in sys.modules:
    pg = ModuleType("psycopg2")
    pg.pool = ModuleType("psycopg2.pool")
    pg.extras = ModuleType("psycopg2.extras")
    pg.pool.ThreadedConnectionPool = MagicMock
    pg.extras.RealDictCursor = MagicMock
    sys.modules["psycopg2"] = pg
    sys.modules["psycopg2.pool"] = pg.pool
    sys.modules["psycopg2.extras"] = pg.extras

# confluent_kafka may not be installed either.
if "confluent_kafka" not in sys.modules:
    ck = ModuleType("confluent_kafka")
    ck.Producer = MagicMock
    ck.Consumer = MagicMock
    ck.KafkaError = MagicMock
    sys.modules["confluent_kafka"] = ck
