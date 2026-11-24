SELECT id, latitude, longitude, radius_m, note, created_at
FROM public.location_blacklist
WHERE enabled
  AND $__timeFilter(created_at)
ORDER BY id;
