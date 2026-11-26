SELECT count(*) FILTER (WHERE action <> 'skip') AS "Ответы",
       count(DISTINCT card_id) FILTER (WHERE action <> 'skip') AS "Уникальные карточки",
       count(DISTINCT (reviewed_at AT TIME ZONE 'Europe/Moscow')::date)
           FILTER (WHERE action <> 'skip') AS "Дни с занятиями",
       (SELECT count(*) FROM english_report_sessions WHERE $__timeFilter(started_at)) AS "Подходы"
FROM english_report_events
WHERE $__timeFilter(reviewed_at);
