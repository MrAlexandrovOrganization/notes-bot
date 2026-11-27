SELECT links AS "Связи", rating AS "Оценка", day::text AS "Дата"
FROM journal.daily
WHERE rating IS NOT NULL AND $__timeFilter(day::timestamp AT TIME ZONE 'Europe/Moscow')
ORDER BY day;
