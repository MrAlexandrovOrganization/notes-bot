SELECT id, latitude, longitude, radius_m, note, created_at
FROM public.location_blacklist
WHERE enabled
ORDER BY id;
