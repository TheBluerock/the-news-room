-- LEARN-01 Option C: quality scores feed back into agent context.
-- Grant learner_rw read access to analytics quality scores so SearchSimilar
-- can weight results by quality and the quality-summary endpoint can aggregate.

GRANT USAGE ON SCHEMA analytics_svc TO learner_rw;
GRANT SELECT ON analytics_svc.article_performance TO learner_rw;
