WITH latest AS (
    SELECT recorded_at, latitude, longitude, accuracy
    FROM public.locations
    WHERE source = 'android' AND NOT hidden AND quality_reason = ''
      AND accuracy > 0 AND accuracy <= 100
    ORDER BY recorded_at DESC, id DESC
    LIMIT 1
), measured AS (
    SELECT p.*, s.state, s.candidate, s.candidate_since, s.last_observed_at,
           l.recorded_at, l.accuracy AS accuracy_m,
           extract(epoch FROM now()-l.recorded_at) AS age_seconds,
           CASE WHEN l.recorded_at IS NOT NULL THEN
             12742000.0 * asin(sqrt(least(1.0, greatest(0.0,
               power(sin(radians(l.latitude-p.latitude)/2),2)
               + cos(radians(p.latitude))*cos(radians(l.latitude))
                 * power(sin(radians(l.longitude-p.longitude)/2),2)
           )))) END AS distance_m
    FROM public.geo_places p
    LEFT JOIN public.geo_states s ON s.place_id = p.id
    LEFT JOIN latest l ON true
    WHERE p.enabled AND p.id::text = ${place:sqlstring}
)
SELECT name, radius_m, radius_m + greatest(50.0,radius_m*0.25) AS exit_radius_m,
       confirmation_seconds, distance_m, accuracy_m, age_seconds,
       recorded_at, last_observed_at,
       CASE WHEN last_observed_at IS NULL OR last_observed_at < now()-interval '5 minutes'
                THEN 'Нет свежего подтверждения'
            WHEN state = 'inside' THEN 'Внутри'
            WHEN state = 'outside' THEN 'Снаружи'
            ELSE 'Не определено' END AS confirmed_state,
       CASE WHEN recorded_at IS NULL THEN 'Нет пригодной GPS-точки'
            WHEN age_seconds > 180 THEN 'GPS-точка устарела'
            WHEN age_seconds < -30 THEN 'GPS-точка из будущего'
            WHEN distance_m + accuracy_m <= radius_m THEN 'Достоверно внутри'
            WHEN distance_m - accuracy_m >= radius_m + greatest(50.0,radius_m*0.25)
                THEN 'Достоверно снаружи'
            ELSE 'Неопределённая граница' END AS latest_position,
       CASE candidate WHEN 'inside' THEN 'Ожидается подтверждение входа'
                      WHEN 'outside' THEN 'Ожидается подтверждение выхода'
                      ELSE 'Нет' END AS pending_transition,
       candidate_since
FROM measured;
