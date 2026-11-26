WITH dates AS (
    SELECT generate_series(
        date_trunc('day', $__timeFrom()::timestamptz AT TIME ZONE 'Europe/Moscow'),
        date_trunc('day', $__timeTo()::timestamptz AT TIME ZONE 'Europe/Moscow'),
        interval '1 day'
    ) AS day
), totals AS (
    SELECT date_trunc('day', reviewed_at AT TIME ZONE 'Europe/Moscow') AS day,
           count(*) FILTER (WHERE action = 'miss') AS misses,
           count(*) FILTER (WHERE action = 'hard') AS hard_answers,
           count(*) FILTER (WHERE action = 'easy') AS easy_answers,
           count(*) FILTER (WHERE action = 'skip') AS skips
    FROM english_report_events
    WHERE $__timeFilter(reviewed_at)
    GROUP BY 1
)
SELECT greatest(dates.day AT TIME ZONE 'Europe/Moscow', $__timeFrom()::timestamptz) AS time,
       coalesce(misses, 0) AS "Не вспомнил",
       coalesce(hard_answers, 0) AS "С трудом",
       coalesce(easy_answers, 0) AS "Легко",
       coalesce(skips, 0) AS "Пропустил"
FROM dates
LEFT JOIN totals USING (day)
ORDER BY time;
