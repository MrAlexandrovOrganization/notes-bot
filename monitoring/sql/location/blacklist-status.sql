SELECT
    (SELECT count(*) FROM public.location_blacklist WHERE enabled) AS active_rules,
    (SELECT count(*) FROM public.location_blacklist WHERE NOT enabled) AS disabled_rules,
    CASE WHEN EXISTS (SELECT FROM public.location_blacklist WHERE enabled)
         THEN (SELECT count(*) FROM public.locations
               WHERE hidden AND public.location_blacklist_matches(latitude,longitude))
         ELSE 0 END AS matched_points;
