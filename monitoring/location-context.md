# Location · контекст

UID location-context. Source template: templates/location-context.json; SQL in
sql/location-context. Requires Location migration 013 and the existing PostgreSQL
datasource. `make monitoring-dashboard monitoring-check observability-check` builds
and validates the versioned resource. Deploy only the matching CI resource artifact;
no application restart is needed for the Grafana resource itself.

The dashboard shows Android/iOS context coverage, passive decisions, observation
age and a point table (both hidden and visible, up to 3000 rows in the selected time
range). Missing data remains missing. Step counts are interval observations: do not
sum repeated or overlapping windows. Negative ages reveal observation times later
than the fix. Authoritative device identity is a server credential slot, not a client
supplied installation UUID. New context has no effect on point hiding or blacklist.
