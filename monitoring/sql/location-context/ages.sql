SELECT recorded_at AS "time", device_id AS device, steps_age_s AS "Возраст интервала шагов, с",
       movement_age_s AS "Возраст события движения, с"
FROM public.location_context
WHERE $__timeFilter(recorded_at) AND context_status='valid'
  AND device_id IN (${device:sqlstring})
ORDER BY recorded_at;
