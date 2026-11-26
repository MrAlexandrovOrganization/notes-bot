SELECT
  recorded_at AS "time",
  latitude,
  longitude,
  accuracy,
  date,
  source::text AS source
FROM locations
WHERE NOT hidden
  AND source = 'unknown'
  AND 'unknown' IN (${source:sqlstring})
  AND $__timeFilter(recorded_at)
ORDER BY recorded_at, id;
