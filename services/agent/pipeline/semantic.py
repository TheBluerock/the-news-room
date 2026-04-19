"""Redis HNSW semantic search for article embeddings per market namespace."""
import logging

import redis as redis_lib

logger = logging.getLogger("agent.pipeline.semantic")


def search(rdb: redis_lib.Redis, market: str, query_vector: list[float] | None, topic_name: str, k: int = 5) -> list[dict]:
    """
    KNN search on vectors:<market>:* index.
    Skips entries where vector:stale:<article_id> exists.
    Falls back to empty list if index not yet built.
    """
    if query_vector is None:
        # Phase 3: embeddings generated in Phase 3b (Learner). Return empty for now.
        logger.debug("no query vector — skipping semantic search")
        return []

    try:
        results = rdb.execute_command(
            "FT.SEARCH", f"idx:vectors:{market}",
            f"(@vector:[VECTOR_RANGE 0.4 $vec])",
            "PARAMS", "2", "vec", _encode_vector(query_vector),
            "LIMIT", "0", str(k),
            "DIALECT", "2",
        )
        articles = []
        for i in range(1, len(results), 2):
            article_id = results[i].decode() if isinstance(results[i], bytes) else results[i]
            # Skip stale (Learner is regenerating embeddings)
            if rdb.exists(f"vector:stale:{article_id}"):
                continue
            articles.append({"article_id": article_id})
        return articles
    except Exception as e:
        logger.warning("HNSW search failed: %s", e)
        return []


def _encode_vector(v: list[float]) -> bytes:
    import struct
    return struct.pack(f"{len(v)}f", *v)
