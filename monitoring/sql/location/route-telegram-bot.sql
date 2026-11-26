SELECT
  recorded_at AS "time",
  latitude,
  longitude,
  accuracy,
  date,
  source::text AS source
FROM locations
WHERE NOT hidden
  AND source = 'telegram-bot'
  AND 'telegram-bot' IN (${source:sqlstring})
  AND $__timeFilter(recorded_at)
ORDER BY recorded_at, id;
