SELECT started_at AS "Начало",
       ended_at AS "Последний ответ",
       attempts AS "Ответы",
       unique_cards AS "Уникальные карточки",
       misses AS "Не вспомнил",
       hard_answers AS "С трудом",
       easy_answers AS "Легко"
FROM english_report_sessions
WHERE $__timeFilter(started_at)
ORDER BY started_at DESC;
