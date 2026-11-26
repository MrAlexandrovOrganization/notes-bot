SELECT context_status AS status, count(*) AS points
FROM public.location_context
WHERE $__timeFilter(recorded_at) AND source IN ('android','ios')
  AND source::text IN (${source:sqlstring})
  AND COALESCE(device_id,'legacy:' || source::text) IN (${device:sqlstring})
GROUP BY 1 ORDER BY 2 DESC;
