SELECT recorded_at AS "time", id, latitude, longitude, accuracy, date,
       source::text AS source, provider, hidden, quality_reason,
       public.location_blacklist_matches(latitude, longitude) AS blacklisted
FROM locations
WHERE source::text IN (${source:sqlstring})
  AND $__timeFilter(recorded_at)
ORDER BY recorded_at, id;
