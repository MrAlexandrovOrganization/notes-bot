SELECT $__timeGroupAlias(recorded_at, '5m'),
       100.0 * count(*) FILTER (WHERE context_status='valid') / NULLIF(count(*),0) AS "С контекстом, %"
FROM public.location_context
WHERE $__timeFilter(recorded_at) AND source IN ('android','ios')
  AND source::text IN (${source:sqlstring})
  AND COALESCE(device_id,'legacy:' || source::text) IN (${device:sqlstring})
GROUP BY 1 ORDER BY 1;
