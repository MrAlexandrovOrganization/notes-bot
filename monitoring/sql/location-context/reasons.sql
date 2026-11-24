SELECT COALESCE(passive_reason,'Нет данных') AS reason, count(*) AS points
FROM public.location_context
WHERE $__timeFilter(recorded_at) AND source='android'
  AND COALESCE(device_id,'legacy:' || source::text) IN (${device:sqlstring})
GROUP BY 1 ORDER BY 2 DESC;
