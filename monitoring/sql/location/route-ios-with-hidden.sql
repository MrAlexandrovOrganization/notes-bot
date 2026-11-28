SELECT
  recorded_at AS "time",
  id,
  latitude,
  longitude,
  accuracy,
  date,
  source::text AS source,
  hidden,
  quality_reason
FROM locations
WHERE (NOT hidden OR NOT public.location_blacklist_matches(latitude, longitude))
  AND source = 'ios'
  AND 'ios' IN (${source:sqlstring})
  AND $__timeFilter(recorded_at)
ORDER BY recorded_at, id;
