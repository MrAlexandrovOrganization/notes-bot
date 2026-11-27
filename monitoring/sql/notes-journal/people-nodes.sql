WITH people AS (
    SELECT first_id AS id, first_name AS title, day
    FROM journal.people_pairs
    WHERE $__timeFilter(day::timestamp AT TIME ZONE 'Europe/Moscow')
    UNION ALL
    SELECT second_id AS id, second_name AS title, day
    FROM journal.people_pairs
    WHERE $__timeFilter(day::timestamp AT TIME ZONE 'Europe/Moscow')
)
SELECT id, max(title) AS title, count(DISTINCT day) AS mainstat
FROM people
GROUP BY id;
