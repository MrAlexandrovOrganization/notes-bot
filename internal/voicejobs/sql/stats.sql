SELECT
    count(*) FILTER (WHERE NOT done),
    count(*) FILTER (WHERE NOT done AND payload->>'result_ready' = 'true'),
    count(*) FILTER (WHERE NOT done AND cancel_requested),
    GREATEST(COALESCE(EXTRACT(EPOCH FROM now() - min(created_at) FILTER (WHERE NOT done)), 0), 0)::double precision
FROM voice_jobs;
