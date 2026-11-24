SELECT id::text, name, latitude, longitude, radius_m, confirmation_seconds
FROM public.geo_places
WHERE enabled
ORDER BY name, id;
