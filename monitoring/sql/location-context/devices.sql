-- Include both configured credential slots before the first enriched measurement arrives.
SELECT DISTINCT device AS __text, device AS __value FROM (
    SELECT COALESCE(device_id, 'legacy:' || source::text) AS device
    FROM public.location_context WHERE source IN ('android','ios')
    UNION ALL SELECT 'mobile:android:primary'
    UNION ALL SELECT 'mobile:ios:primary'
) devices ORDER BY 1;
