SELECT recorded_at AS "time", id, source::text AS platform,
       COALESCE(device_id,'legacy:' || source::text) AS device,
       installation_id, app_version, context_status, steps_status,
       steps_count, steps_from, steps_to, steps_age_s,
       movement_status, movement_event, movement_at, movement_age_s,
       collection_mode, requested_interval_ms / 1000.0 AS requested_interval_s,
       delivery_channel, passive_reason, step_detector_status, step_counter_status,
       hidden, quality_reason, latitude, longitude, accuracy
FROM public.location_context
WHERE $__timeFilter(recorded_at)
  AND source IN ('android','ios')
  AND source::text IN (${source:sqlstring})
  AND COALESCE(device_id,'legacy:' || source::text) IN (${device:sqlstring})
ORDER BY recorded_at DESC
LIMIT 3000;
