SELECT
  recorded_at AS "time",
  id,
  latitude,
  longitude,
  accuracy,
  date
FROM locations
WHERE not hidden
  AND $__timeFilter(recorded_at)
ORDER BY recorded_at
