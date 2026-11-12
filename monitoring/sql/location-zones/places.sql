SELECT name AS __text, id::text AS __value
FROM public.geo_places
WHERE enabled
ORDER BY name, id;
