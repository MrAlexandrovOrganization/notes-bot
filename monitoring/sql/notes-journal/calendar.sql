WITH activity AS (
    SELECT day, characters AS amount, 'симв.' AS unit
    FROM journal.daily
    WHERE ${activity:sqlstring} = 'characters'
    UNION ALL
    SELECT day, links AS amount, 'связей' AS unit
    FROM journal.daily
    WHERE ${activity:sqlstring} = 'links'
    UNION ALL
    SELECT day, 1 AS amount, 'Зал' AS unit
    FROM journal.gym
    WHERE ${activity:sqlstring} = 'gym'
), dated AS (
    SELECT day::timestamp AT TIME ZONE 'Europe/Moscow' AS "time", amount, unit
    FROM activity
    WHERE amount > 0
)
SELECT "time",
    CASE unit WHEN 'Зал' THEN unit ELSE concat(amount, ' ', unit) END AS activity,
    CASE unit
        WHEN 'симв.' THEN
            CASE WHEN amount >= 2000 THEN 4 WHEN amount >= 750 THEN 3 WHEN amount >= 200 THEN 2 ELSE 1 END
        WHEN 'связей' THEN
            CASE WHEN amount >= 8 THEN 4 WHEN amount >= 4 THEN 3 WHEN amount >= 2 THEN 2 ELSE 1 END
        ELSE 4
    END AS intensity
FROM dated
WHERE $__timeFilter("time")
ORDER BY "time";
