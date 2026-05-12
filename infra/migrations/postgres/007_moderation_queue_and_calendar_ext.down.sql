ALTER TABLE analytics_svc.editorial_calendar
    DROP COLUMN IF EXISTS angle,
    DROP COLUMN IF EXISTS source_url,
    DROP COLUMN IF EXISTS journalist_profile_id;

DROP TABLE IF EXISTS moderation_svc.review_queue;
DROP SCHEMA IF EXISTS moderation_svc;
