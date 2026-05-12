"""Unit tests for moderation consumer — quality score recording wiring."""
import uuid
from unittest.mock import MagicMock, patch, call


def _make_event(**overrides):
    base = {
        "article_id": str(uuid.uuid4()),
        "market": "italy",
        "language": "it",
        "content": "Un grande vino rosso con note di frutta e spezie.",
        "title": "Barolo 2023",
        "excerpt": "Eccellente annata.",
        "section": "degustazioni",
        "author": "Marco Rossi",
        "tags": ["barolo", "piemonte"],
        "slug": "barolo-2023",
        "topic_name": "Barolo",
        "trace_id": "",
    }
    base.update(overrides)
    return base


def test_record_quality_called_on_approve():
    """RecordQualityScore must be called after auto-approve."""
    import consumer as consumer_mod

    event = _make_event()
    producer = MagicMock()
    analytics_channel = MagicMock()
    dlq = MagicMock()

    with patch("consumer.checks.check_cultural", return_value=(True, "")), \
         patch("consumer.checks.check_factual", return_value=(True, 0.9, [])), \
         patch("consumer.checks.score_quality", return_value=0.85), \
         patch("consumer.analytics_client.record_quality") as mock_record, \
         patch("consumer._save_to_queue"), \
         patch("consumer._publish_approved"):

        consumer_mod._process(event, MagicMock(), producer, analytics_channel, dlq, "broker:9092")

    mock_record.assert_called_once_with(
        analytics_channel,
        event["article_id"],
        event["market"],
        0.85,
    )


def test_record_quality_called_on_reject():
    """RecordQualityScore must be called after auto-reject too."""
    import consumer as consumer_mod

    event = _make_event()
    analytics_channel = MagicMock()

    with patch("consumer.checks.check_cultural", return_value=(False, "cultural_insensitivity")), \
         patch("consumer.checks.check_factual", return_value=(False, 0.3, ["wrong_appellation"])), \
         patch("consumer.checks.score_quality", return_value=0.3), \
         patch("consumer.analytics_client.record_quality") as mock_record, \
         patch("consumer._save_to_queue"), \
         patch("consumer._publish_rejected"):

        consumer_mod._process(event, MagicMock(), MagicMock(), analytics_channel, MagicMock(), "broker:9092")

    mock_record.assert_called_once_with(
        analytics_channel,
        event["article_id"],
        event["market"],
        0.3,
    )


def test_record_quality_failure_does_not_crash():
    """analytics_client.record_quality failure must not raise — moderation continues."""
    import consumer as consumer_mod

    event = _make_event()

    with patch("consumer.checks.check_cultural", return_value=(True, "")), \
         patch("consumer.checks.check_factual", return_value=(True, 0.9, [])), \
         patch("consumer.checks.score_quality", return_value=0.8), \
         patch("consumer.analytics_client.record_quality", side_effect=Exception("gRPC down")), \
         patch("consumer._save_to_queue"), \
         patch("consumer._publish_approved"):

        # Must not raise even if analytics is down
        consumer_mod._process(event, MagicMock(), MagicMock(), MagicMock(), MagicMock(), "broker:9092")
