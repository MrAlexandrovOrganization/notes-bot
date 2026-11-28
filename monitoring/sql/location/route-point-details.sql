SELECT recorded_at AS "time", id, latitude, longitude, accuracy, date,
       source::text AS source, provider, hidden, quality_reason,
       public.location_blacklist_matches(latitude, longitude) AS blacklisted
FROM locations
WHERE source::text IN (${source:sqlstring})
  AND $__timeFilter(recorded_at)
  AND (
    NOT hidden
    OR (${show_blocked:sqlstring} = 'yes' AND public.location_blacklist_matches(latitude, longitude))
    OR (${show_hidden:sqlstring} = 'yes' AND NOT public.location_blacklist_matches(latitude, longitude))
  )
ORDER BY recorded_at, id;
