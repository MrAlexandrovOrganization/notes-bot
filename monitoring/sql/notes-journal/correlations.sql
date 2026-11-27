SELECT count(*) AS "Дней с оценкой",
       round(corr(rating, characters)::numeric, 2) AS "Оценка и символы",
       round(corr(rating, links)::numeric, 2) AS "Оценка и связи"
FROM journal.daily
WHERE rating IS NOT NULL AND $__timeFilter(day::timestamp AT TIME ZONE 'Europe/Moscow');
