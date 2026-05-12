"""Fetches market quality summary from Learner REST API (port 8088)."""
import logging
import urllib.request
import urllib.error
import json

logger = logging.getLogger("agent.pipeline.quality")


def fetch(learner_rest_url: str, market: str) -> dict:
    """
    GET {learner_rest_url}/api/quality-summary?market={market}
    Returns dict with avg_quality_score, article_count_30d, top_rejections.
    Falls back to neutral defaults on any error so pipeline is never blocked.
    """
    _default = {"avg_quality_score": 0.5, "article_count_30d": 0, "top_rejections": []}
    try:
        url = f"{learner_rest_url}/api/quality-summary?market={market}"
        with urllib.request.urlopen(url, timeout=3) as resp:
            data = json.loads(resp.read().decode())
            return {
                "avg_quality_score": float(data.get("AvgQualityScore", 0.5)),
                "article_count_30d": int(data.get("ArticleCount30d", 0)),
                "top_rejections": data.get("TopRejections") or [],
            }
    except Exception as e:
        logger.warning("quality summary fetch failed market=%s: %s", market, e)
        return _default
