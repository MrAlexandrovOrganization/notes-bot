SELECT
  recorded_at AS "time",
  id,
  latitude,
  longitude,
  accuracy,
  date,
  source::text AS source
FROM locations
WHERE not hidden
  AND $__timeFilter(recorded_at)
  AND source::text IN (${source:sqlstring})
ORDER BY recorded_at
