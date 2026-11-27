SELECT day::timestamp AT TIME ZONE 'Europe/Moscow' AS "time", characters AS "Символы"
FROM journal.daily
WHERE $__timeFilter(day::timestamp AT TIME ZONE 'Europe/Moscow')
ORDER BY day;
