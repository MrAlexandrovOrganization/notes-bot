SELECT $__timeGroupAlias(received_at, '$__interval', 0),
       count(*) * 60000.0 / $__interval_ms AS value,
       CASE source
           WHEN 'android' THEN 'Android'
           WHEN 'ios' THEN 'iPhone'
           WHEN 'telegram-bot' THEN 'Telegram'
           ELSE 'Неизвестный'
       END || CASE WHEN hidden THEN ' · скрытые' ELSE ' · видимые' END AS metric
FROM public.locations
WHERE $__timeFilter(received_at)
  AND source::text IN (${source:sqlstring})
GROUP BY 1, 3
ORDER BY 1, 3;
