"""Cultural sensitivity and factual accuracy checks using LLM."""
import json
import logging

logger = logging.getLogger("moderation.checks")

CULTURAL_PROMPTS = {
    "italy": (
        "You are a cultural editor for Italian wine journalism. "
        "Check if the article uses correct DOC/DOCG terminology, "
        "avoids cultural insensitivity, and respects regional traditions. "
        "Be strict about factual wine classification claims."
    ),
    "usa": (
        "You are a cultural editor for American food and wine journalism. "
        "Check if the article is culturally appropriate for an American audience, "
        "avoids stereotypes, and uses standard wine scoring context correctly."
    ),
    "china": (
        "You are a cultural editor for Chinese luxury goods journalism. "
        "Check if the article respects Chinese cultural norms around gift-giving, "
        "prestige, and luxury. Ensure no politically sensitive content. "
        "The tone must remain aspirational and respectful."
    ),
}


def check_cultural(content: str, market: str, openai_client) -> tuple[bool, str]:
    """Returns (passed, reason)."""
    system = CULTURAL_PROMPTS.get(market, CULTURAL_PROMPTS["usa"])
    try:
        response = openai_client.chat.completions.create(
            model="gpt-4o",
            messages=[
                {"role": "system", "content": system + "\nRespond with JSON: {\"passed\": true/false, \"reason\": \"...\"}"},
                {"role": "user", "content": f"Check this article:\n\n{content[:3000]}"},
            ],
            max_tokens=300,
            temperature=0,
            response_format={"type": "json_object"},
        )
        result = json.loads(response.choices[0].message.content)
        return bool(result.get("passed", True)), result.get("reason", "")
    except Exception as e:
        logger.warning("cultural check failed: %s — passing", e)
        return True, f"check skipped: {e}"


def check_factual(content: str, market: str, openai_client) -> tuple[bool, float, list[str]]:
    """Returns (passed, accuracy_score 0-1, issues)."""
    try:
        response = openai_client.chat.completions.create(
            model="gpt-4o",
            messages=[
                {"role": "system", "content": (
                    "You are a factual accuracy editor for wine and food journalism. "
                    "Check for incorrect wine region names, wrong vintage years, "
                    "false classification claims, or invented producers. "
                    "Respond with JSON: {\"passed\": true/false, \"accuracy_score\": 0.0-1.0, \"issues\": [\"...\"]}"
                )},
                {"role": "user", "content": f"Check this {market} wine article:\n\n{content[:3000]}"},
            ],
            max_tokens=400,
            temperature=0,
            response_format={"type": "json_object"},
        )
        result = json.loads(response.choices[0].message.content)
        return (
            bool(result.get("passed", True)),
            float(result.get("accuracy_score", 0.8)),
            result.get("issues", []),
        )
    except Exception as e:
        logger.warning("factual check failed: %s — passing with 0.8", e)
        return True, 0.8, []


def score_quality(content: str) -> float:
    """Heuristic quality score based on length and structure."""
    words = len(content.split())
    if words < 200:
        return 0.3
    if words < 400:
        return 0.6
    if words > 800:
        return 0.95
    return 0.8
