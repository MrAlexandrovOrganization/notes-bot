SELECT recorded_at AS "time", id, latitude, longitude, accuracy, date,
       source::text AS source, hidden, quality_reason
FROM locations
WHERE __CONDITION__
  AND source = '__SOURCE__'
  AND '__SOURCE__' IN (${source:sqlstring})
  AND $__timeFilter(recorded_at)
ORDER BY recorded_at, id;
