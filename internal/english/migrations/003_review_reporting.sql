CREATE VIEW english_report_events AS
SELECT card_id, reviewed_at, action
FROM english_review_history;

CREATE VIEW english_report_sessions AS
WITH gaps AS (
    SELECT reviewed_at, card_id, action, token,
           reviewed_at - lag(reviewed_at) OVER (ORDER BY reviewed_at, card_id, token) AS gap
    FROM english_review_history
    WHERE action <> 'skip'
), grouped AS (
    SELECT reviewed_at, card_id, action,
           count(*) FILTER (WHERE gap IS NULL OR gap > interval '30 minutes')
               OVER (ORDER BY reviewed_at, card_id, token ROWS UNBOUNDED PRECEDING) AS session_id
    FROM gaps
)
SELECT min(reviewed_at) AS started_at,
       max(reviewed_at) AS ended_at,
       count(*) AS attempts,
       count(DISTINCT card_id) AS unique_cards,
       count(*) FILTER (WHERE action = 'miss') AS misses,
       count(*) FILTER (WHERE action = 'hard') AS hard_answers,
       count(*) FILTER (WHERE action = 'easy') AS easy_answers
FROM grouped
GROUP BY session_id;
