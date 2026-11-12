-- Every hidden location outside enabled exact/radius rules, without proposal thresholds.
-- Read-only review: a hidden measurement is not proof of a permanently bad coordinate.
SELECT l.recorded_at AS "time", l.id, l.latitude, l.longitude, l.accuracy,
       l.source::text AS source, l.provider, l.quality_reason,
       CASE l.quality_reason
           WHEN '' THEN 'Причина не записана'
           WHEN 'speed' THEN 'Превышение скорости'
           WHEN 'corroborated_jump' THEN 'Подтверждённый скачок'
           WHEN 'extreme_jump' THEN 'Экстремальный скачок'
           WHEN 'uncertain_jump' THEN 'Неподтверждённый скачок'
           WHEN 'poor_accuracy' THEN 'Большая погрешность'
           WHEN 'stale_measurement' THEN 'Запоздалое измерение'
           WHEN 'duplicate_measurement' THEN 'Повтор измерения'
           WHEN 'out_of_order_measurement' THEN 'Нарушен порядок измерений'
           WHEN 'invalid_measurement' THEN 'Некорректное измерение'
           ELSE l.quality_reason
       END AS reason
FROM public.locations l
WHERE l.hidden
  AND l.latitude BETWEEN -90 AND 90 AND l.longitude BETWEEN -180 AND 180
  AND NOT public.location_blacklist_matches(l.latitude, l.longitude)
ORDER BY l.recorded_at DESC, l.id;
