WITH activity AS (
    SELECT day, rating AS amount, 'Оценка' AS unit
    FROM journal.daily
    WHERE ${activity:sqlstring} = 'rating' AND rating IS NOT NULL
    UNION ALL
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
    WHERE amount > 0 OR unit = 'Оценка'
)
SELECT "time",
    CASE unit
        WHEN 'Оценка' THEN concat(unit, ' ', amount, '/10')
        WHEN 'Зал' THEN unit
        ELSE concat(amount, ' ', unit)
    END AS activity,
    -- One calendar palette uses 0–10 for ratings and 11–14 for activity.
    CASE unit
        WHEN 'Оценка' THEN amount
        WHEN 'симв.' THEN
            CASE WHEN amount >= 2000 THEN 14 WHEN amount >= 750 THEN 13 WHEN amount >= 200 THEN 12 ELSE 11 END
        WHEN 'связей' THEN
            CASE WHEN amount >= 8 THEN 14 WHEN amount >= 4 THEN 13 WHEN amount >= 2 THEN 12 ELSE 11 END
        ELSE 14
    END AS intensity
FROM dated
WHERE $__timeFilter("time")
ORDER BY "time";
