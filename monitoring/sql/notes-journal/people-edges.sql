SELECT first_id || '-' || second_id AS id,
       first_id AS source,
       second_id AS target,
       count(*) AS mainstat,
       least(1 + ln(count(*)::double precision), 6) AS thickness
FROM journal.people_pairs
WHERE $__timeFilter(day::timestamp AT TIME ZONE 'Europe/Moscow')
GROUP BY first_id, second_id;
