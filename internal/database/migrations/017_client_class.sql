-- traffic_class used to carry two unrelated facts: what kind of client sent the
-- event, and whether it came from a network an administrator had registered.
-- The second overwrote the first, so on an on-premise deployment — where most
-- traffic is internal by construction — the user guide's own advice for
-- excluding an uptime monitor ("filter to traffic.class = normal") removed the
-- entire workforce, and a crawler on the intranet was filed as internal traffic
-- rather than as a crawler.
--
-- v0.34.33 stopped the overwrite. That alone would have moved the meaning of a
-- saved segment in the middle of its own window: the same employee is
-- internal_traffic before the release and normal after it, so a report spanning
-- the date changes its definition halfway through and nothing on the screen
-- says so.
--
-- client_class is the answer to "what kind of client", read the same way on both
-- sides of that line. An event stored as internal_traffic is an event whose
-- client was never classified as anything else — that is what the overwrite
-- means — so it reads as normal here. raw_events.traffic_class keeps what was
-- actually written, which is what the tracking debugger shows.
DROP VIEW analytics_events;
CREATE VIEW analytics_events AS
SELECT
  e.*,
  CASE WHEN e.traffic_class = 'internal_traffic' THEN 'normal' ELSE e.traffic_class END AS client_class,
  coalesce(i.user_id,e.user_id) AS canonical_user_id,
  CASE WHEN u.user_properties IS NOT NULL AND u.user_properties <> '{}'::jsonb THEN u.user_properties ELSE e.user_properties END AS canonical_user_properties,
  CASE
    WHEN coalesce(i.user_id,e.user_id) IS NOT NULL THEN 'u:' || coalesce(i.user_id,e.user_id)
    ELSE 'v:' || e.visitor_id
  END AS entity_id
FROM raw_events e
LEFT JOIN visitor_identities i ON i.site_id=e.site_id AND i.visitor_id=e.visitor_id
LEFT JOIN identified_users u ON u.site_id=e.site_id AND u.user_id=coalesce(i.user_id,e.user_id);
