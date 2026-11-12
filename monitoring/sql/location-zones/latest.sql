SELECT recorded_at, latitude, longitude, accuracy AS accuracy_m,
       extract(epoch FROM now()-recorded_at) AS age_seconds
FROM public.locations
WHERE source = 'android' AND NOT hidden AND quality_reason = ''
  AND accuracy > 0 AND accuracy <= 100
ORDER BY recorded_at DESC, id DESC
LIMIT 1;
