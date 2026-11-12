SELECT p.id::text, p.name, p.radius_m,
       p.radius_m + greatest(50.0,p.radius_m*0.25) AS exit_radius_m,
       p.confirmation_seconds, p.latitude, p.longitude,
       CASE WHEN s.last_observed_at IS NULL OR s.last_observed_at < now()-interval '5 minutes'
                THEN 'Нет свежего подтверждения'
            WHEN s.state = 'inside' THEN 'Внутри'
            WHEN s.state = 'outside' THEN 'Снаружи'
            ELSE 'Не определено' END AS confirmed_state,
       s.last_observed_at
FROM public.geo_places p
LEFT JOIN public.geo_states s ON s.place_id = p.id
WHERE p.enabled
ORDER BY p.name, p.id;
