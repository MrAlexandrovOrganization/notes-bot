SELECT name, latitude, longitude, radius_m, confirmation_seconds
FROM public.geo_places
WHERE enabled AND id::text = ${place:sqlstring};
