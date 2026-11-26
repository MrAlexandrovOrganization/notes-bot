SELECT started_at AS time,
       attempts AS "Ответы",
       unique_cards AS "Уникальные карточки"
FROM english_report_sessions
WHERE $__timeFilter(started_at)
ORDER BY time;
