-- Proposals only: this query does not create or enable blacklist rules.
WITH eligible AS MATERIALIZED (
    SELECT latitude, longitude, hidden, source, recorded_at
    FROM public.locations
    WHERE latitude BETWEEN -90 AND 90 AND longitude BETWEEN -180 AND 180
      AND CASE WHEN EXISTS (SELECT FROM public.location_blacklist WHERE enabled)
        THEN NOT public.location_blacklist_matches(latitude, longitude)
        ELSE true END
), cells AS (
    SELECT round(latitude::numeric, 3)::double precision AS grid_lat,
           round(longitude::numeric, 3)::double precision AS grid_lon,
           avg(latitude) FILTER (WHERE hidden) AS latitude,
           avg(longitude) FILTER (WHERE hidden) AS longitude,
           count(*) FILTER (WHERE hidden) AS hidden_samples,
           count(DISTINCT (recorded_at AT TIME ZONE 'Europe/Moscow')::date)
               FILTER (WHERE hidden) AS days
    FROM eligible
    GROUP BY 1, 2
    HAVING count(*) FILTER (WHERE hidden) >= ${min_points:sqlstring}::integer
       AND count(*) FILTER (WHERE hidden) >= count(*) * 0.95
       AND count(DISTINCT (recorded_at AT TIME ZONE 'Europe/Moscow')::date)
               FILTER (WHERE hidden) >= ${min_days:sqlstring}::integer
), ranked AS MATERIALIZED (
    SELECT *, row_number() OVER (
        ORDER BY days DESC, hidden_samples DESC, grid_lat, grid_lon
    ) AS priority
    FROM cells
), centres AS (
    -- Keep local peaks instead of proposing many neighbouring cells in one area.
    SELECT r.*
    FROM ranked r
    WHERE NOT EXISTS (
        SELECT FROM ranked p
        WHERE p.priority < r.priority
          AND abs(p.latitude-r.latitude) < 0.005
          AND 12742000 * asin(sqrt(least(1.0, greatest(0.0,
              power(sin(radians(p.latitude-r.latitude)/2),2)
              + cos(radians(r.latitude))*cos(radians(p.latitude))
                * power(sin(radians(p.longitude-r.longitude)/2),2)
          )))) < 500
    )
), proposals AS (
    SELECT 'C-' || upper(substr(md5(c.grid_lat::text || ':' || c.grid_lon::text),1,6)) AS candidate_id,
           c.latitude, c.longitude, 100 AS radius_m,
           count(*) AS samples,
           count(*) FILTER (WHERE l.hidden) AS hidden_samples,
           count(*) FILTER (WHERE NOT l.hidden) AS visible_samples,
           count(DISTINCT (l.recorded_at AT TIME ZONE 'Europe/Moscow')::date) AS days,
           min(l.recorded_at) AS first_seen, max(l.recorded_at) AS last_seen,
           string_agg(DISTINCT l.source::text, ', ') AS sources,
           c.priority
    FROM centres c
    JOIN eligible l ON abs(l.latitude-c.latitude) < 0.001
                   AND 12742000 * asin(sqrt(least(1.0,greatest(0.0,
                       power(sin(radians(l.latitude-c.latitude)/2),2)
                       + cos(radians(c.latitude))*cos(radians(l.latitude))
                         * power(sin(radians(l.longitude-c.longitude)/2),2)
                   )))) <= 100
    GROUP BY c.grid_lat,c.grid_lon,c.latitude,c.longitude,c.priority
    HAVING count(*) FILTER (WHERE l.hidden) >= count(*) * 0.95
       AND count(*) FILTER (WHERE l.hidden) >= ${min_points:sqlstring}::integer
       AND count(DISTINCT (l.recorded_at AT TIME ZONE 'Europe/Moscow')::date)
               >= ${min_days:sqlstring}::integer
)
SELECT candidate_id, latitude, longitude, radius_m, samples, hidden_samples,
       visible_samples, round(100.0*hidden_samples/samples,1) AS hidden_percent,
       days, first_seen, last_seen, sources
FROM proposals
ORDER BY days DESC, hidden_samples DESC, candidate_id
LIMIT 20;
