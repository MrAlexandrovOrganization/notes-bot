SELECT day::timestamp AT TIME ZONE 'Europe/Moscow' AS "time", rating AS "Оценка"
FROM journal.daily
WHERE rating IS NOT NULL AND $__timeFilter(day::timestamp AT TIME ZONE 'Europe/Moscow')
ORDER BY day;
