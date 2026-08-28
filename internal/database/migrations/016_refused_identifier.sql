-- The data quality screen counts events that arrive without a user id, so an
-- operator can tell an integration to start identifying people. Since the
-- privacy filter began inspecting the user id, an identifier it refuses is
-- blanked before the durable write — and landed in that same counter.
--
-- Those are opposite problems. One integration is not calling identify() at
-- all; the other is calling it with a phone number. An operator reading the
-- first would tell a team to start doing what that team is already doing.
ALTER TABLE data_quality_daily
  ADD COLUMN refused_user_id bigint NOT NULL DEFAULT 0;
