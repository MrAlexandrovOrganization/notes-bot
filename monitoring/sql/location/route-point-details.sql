SELECT recorded_at AS "time", id, latitude, longitude, accuracy, date,
       source::text AS source, provider, hidden, quality_reason
FROM locations
WHERE source::text IN (${source:sqlstring})
  AND $__timeFilter(recorded_at)
  AND (NOT hidden OR NOT public.location_blacklist_matches(latitude, longitude))
ORDER BY recorded_at, id;
