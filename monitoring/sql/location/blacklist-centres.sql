WITH rules AS NOT MATERIALIZED (
    SELECT id, latitude, longitude, radius_m, note, created_at
    FROM public.location_blacklist
    WHERE enabled AND $__timeFilter(created_at)
),
affected AS (
    SELECT id,
           string_agg(DISTINCT source, ', ' ORDER BY source) AS sources,
           count(*) FILTER (WHERE source IN (${source:sqlstring})) AS selected_points
    FROM (
        SELECT r.id, p.source::text AS source
        FROM rules r
        JOIN public.locations p ON p.latitude = r.latitude AND p.longitude = r.longitude
        WHERE r.radius_m = 0 AND p.hidden
        UNION ALL
        SELECT r.id, p.source::text AS source
        FROM rules r
        JOIN public.locations p
          ON public.location_distance_m(r.latitude, r.longitude, p.latitude, p.longitude) <= r.radius_m
        WHERE r.radius_m > 0 AND p.hidden
    ) matches
    GROUP BY id
)
SELECT r.id, r.latitude, r.longitude, r.radius_m, r.note, r.created_at,
       CASE
           WHEN starts_with(r.note, 'auto_speed_confirmed; source_id=') THEN 'Автоматически: подтверждённый скачок'
           WHEN origin_id.point_id IS NOT NULL THEN 'В заметке указан ID исходной точки'
           ELSE 'См. заметку к правилу'
       END AS rule_origin,
       origin_id.point_id AS origin_point_id,
       COALESCE(origin.source::text, 'Не указан') AS origin_source,
       affected.sources AS affected_sources,
       COALESCE(affected.selected_points, 0) AS selected_points
FROM rules r
CROSS JOIN LATERAL (
    SELECT substring(r.note FROM '(?:^|[;[:space:]])source_id=([^;[:space:]]+)') AS point_id
) origin_id
LEFT JOIN public.locations origin ON origin.id = origin_id.point_id
LEFT JOIN affected ON affected.id = r.id
WHERE ARRAY['android','ios','telegram-bot','unknown'] <@ ARRAY[${source:sqlstring}]
   OR origin.source::text IN (${source:sqlstring})
   OR affected.selected_points > 0
ORDER BY r.id;
