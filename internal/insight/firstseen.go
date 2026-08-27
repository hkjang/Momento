package insight

// FirstSeenCTE returns the body of a subquery reporting, for every person seen in
// one environment before a given instant, the moment they were first seen there.
// It is the single definition of "new", so the landing screen and the insight
// report cannot answer that question differently.
//
// It reads the daily visitor rollup rather than the events. Both answer the same
// question — the rollup's first_seen is maintained with least() by the same
// transaction that stores the event, per environment — but the events version had
// to read every row the site had ever collected before the period ended, once for
// the period and again for the comparison. That was the dominant cost of both
// screens, and it grew with the site's age rather than with the question: the
// rollup holds one row per person per active day instead of one per event.
//
// It is also the more truthful of the two. Raw events expire on their own
// retention policy while the rollups are kept unless a site sets a limit for
// them, so once a person's early events aged out the events version called them
// new again on their next visit.
//
// The identity join matches the analytics_events view exactly: an event carrying
// a user id always writes visitor_identities in the same transaction, so a person
// is their user id when one is known and their visitor id otherwise.
func FirstSeenCTE(site, environment, before string) string {
	return `SELECT CASE
			WHEN coalesce(i.user_id,d.user_id) IS NOT NULL THEN 'u:' || coalesce(i.user_id,d.user_id)
			ELSE 'v:' || d.visitor_id
		END AS entity_id, min(d.first_seen) AS first_at
		FROM daily_site_visitors d
		LEFT JOIN visitor_identities i ON i.site_id=d.site_id AND i.visitor_id=d.visitor_id
		WHERE d.site_id=` + site + ` AND d.environment=` + environment + ` AND d.first_seen < ` + before + `
		GROUP BY 1`
}
