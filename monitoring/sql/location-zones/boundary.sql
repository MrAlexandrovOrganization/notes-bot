-- Each zone has its own closed ring of geodesic vertices.
-- Small markers draw the contour without connecting different zones.
WITH place AS (
    SELECT id, name, radius_m AS boundary_radius_m,
           radians(latitude) AS lat, radians(longitude) AS lon,
           (radius_m) / 6371000.0 AS delta
    FROM public.geo_places
    WHERE enabled
), points AS (
    SELECT p.*, step, radians(step::double precision) AS bearing
    FROM place p CROSS JOIN generate_series(0, 360) AS step
), destinations AS (
    SELECT *, asin(least(1.0, greatest(-1.0,
        sin(lat)*cos(delta) + cos(lat)*sin(delta)*cos(bearing)
    ))) AS dest_lat
    FROM points
), coordinates AS (
    SELECT id, name, boundary_radius_m, step, degrees(dest_lat) AS latitude,
           degrees(lon + atan2(sin(bearing)*sin(delta)*cos(lat),
                              cos(delta)-sin(lat)*sin(dest_lat))) AS longitude
    FROM destinations
)
SELECT id::text, name, boundary_radius_m, step, latitude, longitude
FROM coordinates
ORDER BY id, step;
