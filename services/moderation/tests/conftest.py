"""Stub out heavy native/proto deps before any test module imports api.py or consumer.py."""
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

# openai not installed in test env.
if "openai" not in sys.modules:
    oa = ModuleType("openai")
    oa.OpenAI = MagicMock
    sys.modules["openai"] = oa

# opentelemetry stubs.
for _mod in ("opentelemetry", "opentelemetry.propagate", "opentelemetry.trace"):
    if _mod not in sys.modules:
        sys.modules[_mod] = ModuleType(_mod)
if "opentelemetry.propagate" in sys.modules:
    sys.modules["opentelemetry.propagate"].extract = lambda carrier: None  # type: ignore
    sys.modules["opentelemetry.propagate"].inject = lambda carrier: None  # type: ignore
if "opentelemetry.trace" in sys.modules:
    sys.modules["opentelemetry.trace"].get_tracer = lambda name: MagicMock()  # type: ignore

# grpc stub (no native lib available).
if "grpc" not in sys.modules:
    g = ModuleType("grpc")
    g.Channel = object
    g.insecure_channel = MagicMock
    g.RpcError = Exception
    sys.modules["grpc"] = g

# analytics_client uses google.protobuf + grpcio which aren't installed.
# Stub the whole module so consumer.py can import it; tests patch individual calls.
if "analytics_client" not in sys.modules:
    ac = ModuleType("analytics_client")
    ac.record_quality = MagicMock()
    sys.modules["analytics_client"] = ac

# checks module may import openai internals — stub if needed.
if "checks" not in sys.modules:
    ch = ModuleType("checks")
    ch.check_cultural = MagicMock(return_value=(True, ""))
    ch.check_factual = MagicMock(return_value=(True, 0.9, []))
    ch.score_quality = MagicMock(return_value=0.8)
    sys.modules["checks"] = ch
