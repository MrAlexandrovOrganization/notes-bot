SELECT id, latitude, longitude, radius_m, enabled, note, created_at
FROM public.location_blacklist
ORDER BY enabled DESC, id;
