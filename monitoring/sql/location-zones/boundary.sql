-- Spherical destination points: same Earth radius as the Location detector.
-- A closed 360-segment ring, in metres rather than screen pixels.
WITH place AS (
    SELECT name, latitude, longitude, radius_m,
           radians(latitude) AS lat, radians(longitude) AS lon,
           radius_m / 6371000.0 AS delta
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
SELECT name, radius_m, degrees(dest_lat) AS latitude,
       degrees(lon + atan2(sin(bearing)*sin(delta)*cos(lat),
                          cos(delta)-sin(lat)*sin(dest_lat))) AS longitude
FROM destinations
ORDER BY step;
