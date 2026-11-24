SELECT
  recorded_at AS "time",
  latitude,
  longitude,
  accuracy,
  date
FROM locations
WHERE NOT hidden
  AND $__timeFilter(recorded_at)
ORDER BY recorded_at
