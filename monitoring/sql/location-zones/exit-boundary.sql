-- Outside is confirmed beyond radius + max(50 m, 25% radius), plus GPS accuracy.
WITH place AS (
    SELECT name, radius_m + greatest(50.0, radius_m*0.25) AS exit_radius_m,
           radians(latitude) AS lat, radians(longitude) AS lon,
           (radius_m + greatest(50.0, radius_m*0.25)) / 6371000.0 AS delta
    FROM public.geo_places
    WHERE enabled AND id::text = ${place:sqlstring}
), points AS (
    SELECT p.*, step, radians(step::double precision) AS bearing
    FROM place p CROSS JOIN generate_series(0, 360) AS step
), destinations AS (
    SELECT *, asin(least(1.0, greatest(-1.0,
        sin(lat)*cos(delta) + cos(lat)*sin(delta)*cos(bearing)
    ))) AS dest_lat
    FROM points
)
SELECT name, exit_radius_m, degrees(dest_lat) AS latitude,
       degrees(lon + atan2(sin(bearing)*sin(delta)*cos(lat),
                          cos(delta)-sin(lat)*sin(dest_lat))) AS longitude
FROM destinations
ORDER BY step;
