# Changelog

## v0.32.3

- The in-place upgrade path is now verified in CI. Every release note has promised it and it had only ever been checked by hand. The test rebuilds the schema each past release shipped, fills it with data — a site, events, sessions, an API key, a delivery channel, a scheduled report, an anomaly alert — and applies the current migration set on top, then asserts the upgrade completes, no row was lost, `analytics_events` still resolves over rows written before the columns it reads existed, and a restart re-runs nothing. All thirteen historical points, in 6 seconds.
- The failure this exists for is a migration that is fine on an empty schema and fails on a populated one: a CHECK or NOT NULL that stored rows violate. The service exits when a migration fails, so that upgrade leaves an operator with nothing running. Confirmed the guard catches all three shapes — a violated CHECK, a NOT NULL column with no default, and a migration that quietly deletes rows — by introducing each one and watching it fail.
- `database.MigrateThrough` and `database.Versions` make a historical schema reachable; `Migrate` is unchanged for callers.
- Verified the real upgrade first, v0.21.2 → v0.32.2 on one database: migration 014 applied, all 72 events and 12 sessions intact, the tracking key, server key and personal API key issued by v0.21.2 still authenticated, secrets encrypted by the old release decrypted, and all 43 read endpoints answered 200 over the old data.

## v0.32.2

- Every release now verifies its own offline artifact before publishing. The tarball is the product for an air-gapped deployment and nothing had ever loaded one and started it: the workflow published whatever `docker save` produced. It now loads the archive it is about to publish — not the image still in the local daemon — starts it with the environment variables the release notes give, waits for `/health/ready` so the migrations are known to have run, checks the reported version matches the tag, fetches the console, and logs in as the bootstrapped administrator. A tarball that cannot start is no longer published.
- Verified the current artifact by hand first, following the documented install exactly: checksum, load, run, ready, version, console, login. It worked, so the automation encodes a passing path rather than a fix.

## v0.32.1

- A missed release now repairs itself. A tag push is a one-shot trigger and a platform incident dropped it twice, leaving a tag whose version was announced and whose offline install artifact did not exist — a state nobody notices without looking for it. An hourly job compares recent tags against their releases and dispatches the release workflow for any that is missing, or that has only half of its two assets.
- Only tags from the last seven days are considered: an older tag without a release was a decision by then, and rebuilding it would be surprising.
- The repair path was executed rather than assumed. A reconciler that has only ever reported "nothing to do" has not demonstrated that it can act, so the dispatch was forced once against a cheap target to prove the token, the permission and the call, then reverted.
- No product code changed in this release.

## v0.32.0

- Documented that reports include bot, monitoring and internal traffic. The collector classifies every event by user agent and network and stores the result, and nothing filters on it, so an uptime monitor hitting a page every minute contributes fourteen hundred page views a day to the numbers a reader takes at face value. Nothing in the documentation said so.
- `traffic.internal` is a segment field now. The collector has always recorded whether an event came from a network an administrator marked internal, and nothing could read it — so excluding one's own staff meant naming every internal network by hand.
- The batch size limit and the traffic classification are tested. A batch at the configured limit is accepted, one over it is refused with the limit named, and the four user agent classes come out as expected.
- That reports include every class is now asserted rather than merely true, so changing it later is a deliberate act with a failing test to acknowledge.
- Two workflow fixes from after the v0.31.6 tag: CI and the release workflow both accept a manual trigger, because a platform incident dropped a push and a tag and neither could be recovered. The release trigger needed a second attempt — the first shadowed `GITHUB_REF_NAME`, which the runner overrides, so it tried to release a tag named "main".

## v0.31.6

- Server-side ingestion is tested. A request with no Origin is server to server and must present the site's server API key; the tracking key is not enough there, because it is published in the HTML of every page the site serves. No test had ever sent a request without an Origin, so that rule had never been exercised.
- The fixture stored a literal string in place of the server key hash, so no request could present a valid server key. It now stores a real hash, which is what made the path testable.
- Login rate limiting is tested at the wiring rather than the limiter. The limiter had unit tests; nothing had checked that the login endpoint consults it, which is the only thing between a reachable console and an unlimited password guessing loop.
- Added a manual trigger to the CI workflow. A push to main went unscheduled during a platform incident and, with only push and pull_request triggers, that commit could not be verified afterwards. The trigger worked on its first use.

## v0.31.5

- The personal API key path is tested. Every other test arrives with a session cookie, so the way a BI job or another service actually connects had never been exercised — including the refusals that stop a key being used as an administrator.
- A key reads analytics and the raw export, is refused on administrator endpoints even when its owner is a super administrator, is refused on interactive writes with `SESSION_REQUIRED`, and stops working the moment it is revoked, expires, or its owner is deactivated.
- Removed the Scope column from the API key list. Keys carry a scopes field that nothing reads, and the console never sets it, so the column was always empty while implying the key was restricted. No scope model was invented to fill it; what a key can and cannot do is documented instead.

## v0.31.4

- Access control is tested. Every test had signed in as a super administrator, which short-circuits the workspace membership check entirely, so neither that branch nor any administrator refusal had ever executed. On a shared internal deployment those two rules are what keep one team's analytics out of another team's console.
- An analyst reaches sites in their own workspace and receives 404 for a site in another organisation's — not 403, which would confirm the site exists. That site is also absent from their site list.
- Administrator endpoints refuse an analyst: users, settings, the audit log, the tracking debugger and the query policy.
- Turning visitor profiles off blocks the visitor list, the identity list and the person timeline even for a super administrator, because it is a privacy policy rather than a permission level, while the overview still answers because it names nobody.
- The membership rule is shown to be load-bearing rather than incidentally correct: granting the analyst a role in the other workspace makes the same refused request succeed, and revoking it refuses again.

## v0.31.3

- Environment isolation is tested. Every test had run against a site with only a production environment, so nothing had ever checked that staging traffic stays out of production reports. Most of these queries assemble their environment predicate by string concatenation, which is where one gets dropped; a leak would have appeared as production numbers that were quietly too high. Removing one filter to check makes the test fail, so it is doing its job.
- Adoption's declared target population is tested. Adoption is a rate, and its denominator is an administrator's declared eligible population when one exists and the observed population when it does not. No fixture ever declared a target, so only the fallback had run — and a rate against the wrong denominator is worse than no rate.
- A static sweep for analytical queries missing an environment filter found none; all four candidates build the predicate dynamically. Worth recording as checked rather than assumed.
- Both behaviours were already correct.

## v0.31.2

- The ecommerce funnel is exercised for the first time. It measures four steps and the fixture created only the last one, so the funnel and the cart and checkout user counts had never produced a number.
- The product table is exercised for the first time. It reads an `items` array from the purchase payload and no seeded purchase carried one, so that whole table was empty in every test run. Purchases now carry a realistic array whose price times quantity equals the purchase value, and the assertions check that relationship rather than a fixed total.
- Search refinements, exits and successes are exercised for the first time. Those three figures were zero for want of the event names, not for want of the behaviour.
- All of these were already correct. The point is that the suite could not have told the difference between correct and broken for any of them.

## v0.31.1

- Four reports are verified against known inputs for the first time. The shared fixture created no refund, no engaged-time event, no resource error and no AI call, so the ecommerce refund arithmetic, the engagement path, the resource-error half of the experience report and the whole AI operations report ran, answered zero and passed. All four were correct; that could not be known before.
- Refunds now check that net revenue is revenue minus refunds; engagement checks that an unparseable `active_seconds` is ignored rather than counted or fatal; the experience report checks the resource error count; the AI report checks calls, success rate, token totals, average latency and cost against the seeded values.
- The added fixture events belong to the sessions that already existed for that visitor and day. Writing events without a session row is something the collector never does, and it made the event-derived and table-derived session counts disagree for a reason no deployment would produce — which an earlier test caught.

## v0.31.0

- Completed the OpenAPI document. Thirty-five paths were missing from it, including the page, event and visitor reports, the raw event export, the personal API key surface, user and settings administration, the audit log and every delete and rotate operation. For an on-premise deployment that document is how a BI team or another service learns what the server offers, and nearly a third of it was undescribed.
- Added a test that walks the real router and compares it with the document in both directions: a path the server serves but the document omits, and a path the document describes but the server does not serve. The second matters more, because a reader will build against it.
- The test needs no database, so it runs on every push. The document drifted because it was maintained by hand next to the router with nothing comparing the two.

## v0.30.4

- Verified the search report's numbers against known inputs for the first time. The fixture carried no search events, so nothing in the suite had ever checked a search figure; six searches by five people with one returning nothing and one result clicked now pin the count, the distinct user count, the zero-result rate and the click-through rate.
- Pinned the agreement between the screens and the MCP tools for search and retention. Both pairs own separate copies of their query — the arrangement that let one defect ship three times in the adoption report — and they agree today, so a test is what keeps them agreeing.
- No new disagreement was found in this pass. Search, retention and experience each answer the same from the screen and the tool, which is worth stating rather than implying by silence.

## v0.30.3

- `analyze_feature_adoption` returns the adoption report. It ran its own query and answered with feature events and users, so an agent asked about adoption received no adoption rate, no eligible population and no dormant users — the same defect fixed in the digest last release, in a third place. All three now call one implementation.
- The site's query period limit applies to the funnel and to every MCP tool. It was added to the helper the reports use, and these callers had their own, so a limit lowered to protect the database still left an agent free to ask for five years. Enforcement is the default in both helpers now, and the one caller that must exceed it — privacy deletion, which has to reach as far back as the data goes — says so explicitly.
- Compared the rest of the MCP surface against the screens: `query_metrics` shares the overview's implementation and matches it including session duration, `analyze_experience` agrees with the experience report's impact figures, and `analyze_frustration` carries the same per-signal impact the screen shows.

## v0.30.2

- The adoption digest carries the adoption report. It ran its own query and answered with feature events and users — the feature intelligence report's content under the adoption report's name — so a schedule called Adoption 요약 delivered no adoption rate, no eligible population and no dormant users.
- The adoption computation now lives in one place that the screen and the digest both call, which is the fix for the cause rather than the instance: the digest drifted because it had its own copy of the query.
- Checked the other delivery kinds against their screens: the experience digest's error count and affected users agree with the experience report's impact figures, the AI digest reports what the AI screen reports, and the segment digest is its own definition rather than a screen's.
- The test compares every field of a delivered row against the screen's row rather than checking that the payload is non-empty.

## v0.30.1

- A scheduled report now covers the period the screen it is named after covers. Every delivery measured from the moment the schedule happened to fire, while every screen reads the site's calendar and ends at local midnight, so a seven day digest and the seven day screen described different spans — and the digest's span moved every time the send time drifted. Both windows now come from the same rule.
- The window travels with the payload, so a reader can see what was measured instead of reconstructing it from the send time.
- The overview digest reports sessions and engaged sessions, counted from the sessions table by when they started, which is the definition the screens settled on in v0.29.2. It previously omitted sessions altogether.
- The test asserts the window directly rather than inferring it from counts: whether the counts differ depends on whether any event falls in the band where the two windows disagree, which for the fixture depends on the hour the test runs. Restoring the previous window fails it with a six hour discrepancy.

## v0.30.0

- Separated the two session counts that shared one name. A dimensional breakdown needs the sessions active in a range — the ones that saw a page or arrived from a channel — while the overview reports the ones that began in it, and the difference is every session open at the boundary. On a two day window with one session carried over from before, the overview said six and the query builder said seven.
- `sessions` in the query builder keeps the active meaning that a breakdown requires, and `sessions_started` is the overview's number, so the same question can be asked there and get the same answer.
- The metric picker shows what each metric counts. Two session counts, a conversion count that is events rather than people, and a revenue that reads either property name are not distinguishable from a list of identifiers.
- Checked the report tables while looking for this: the page and event tables count users the same way as the overview, and their view, event and conversion columns sum to the overview totals.

## v0.29.2

- The overview and the insight report agree about sessions. Both answer how many sessions a period had, how many were engaged and how long the average one lasted; the overview measured the span of events inside the query window while the insight report read the sessions table, so the same period was a sixteen minute average session on one screen and twelve on the other.
- Everything about a session now comes from the sessions table, which the collector maintains and which is the only place that knows a session's real span. Sessions are counted by when they started, so consecutive periods add up instead of both claiming a session that spanned midnight.
- The events answer only when no session row exists for the period, which means the derived data is behind rather than that nothing happened. That replaces taking the larger of the two counts, which mixed definitions to avoid showing a zero.
- Session conversion rate now shares that denominator, so the rate and the session count on the same card refer to the same set of sessions.
- Bounded the insight report's first-seen scan, which was missed when the others were bounded in v0.24.2: it read the site's whole history to count new people in a period.
- A test asserts the two screens agree on all three numbers, and fails with 960 against 720 on the previous definitions.

## v0.29.1

- Revenue means the same thing on every screen. A purchase may carry its amount as `value` or `revenue`, and every report read both — except the query builder, which read only the first and answered zero for a site that sends the other. Nothing failed; one screen was simply wrong, which is harder to notice than an error.
- The amount now comes from one definition shared by the overview and the query builder, and a test reads the source to check that no purchase-amount expression accepts only one of the two names, so the next report cannot drift the same way.
- The visitor list says what it counts. It is a per-browser list, so one person using a desktop and a phone appears twice and the row count exceeds the user count on the first screen; the description said neither.
- Documented what the shared metric names mean: user is a person, Visitor is a browser, revenue reads either property, and a rate called 전환율 is user-based while the session-based one is named as such.
- Fixed a fixture that made the biggest integration test fail depending on the hour it ran. Event timestamps were built as now() minus N days plus an hour offset, so after 15:00 UTC two offsets landed on the same Asia/Seoul date, the daily series lost a day and the anomaly baseline came up one sample short. Timestamps are anchored to site-local calendar dates now, and the test passes under UTC, Asia/Seoul and America/Los_Angeles.

## v0.29.0

- Audited every administrative setting for whether anything reads it, after finding last release that the query period limit was enforced in one handler out of twenty-nine. Three settings were accepted, validated, stored, and then read by nothing.
- The aggregate retention limit now deletes. The retention screen offers a period for the daily rollups and the sweep never consulted it, so a site that asked to keep one year of aggregates kept all of them forever. An empty value still means keep indefinitely, so only a site that set a number is affected.
- The site's session timeout now reaches the tracker. Sessions are decided in the browser, and the tracker used its own thirty minute default while the configured value went nowhere; the installed snippet now carries it as `data-session-timeout`. Because the decision is made in the browser, changing the setting requires updating the installed snippet, and the console says so rather than implying it takes effect on its own.
- The realtime retention field is labelled as not applied. Momento keeps no separate realtime store, so there is nothing for the value to trim; it is still accepted for API compatibility, and the screen says what it does instead of leaving a control that silently does nothing.
- Cardinality limits, PII detection mode, retention of raw events and sessions, blocked properties, query string stripping, allowed domains and contract mode were checked and are enforced.

## v0.28.0

- The site's maximum exact query period now applies to every analytical report. It was consulted by one handler — the query builder — while twenty-eight others read whatever range they were asked for, so an administrator who lowered the limit to protect the database still had every heavy report reading without one. The administration screen presented a limit that was not in force.
- A period the policy forbids is refused as `RANGE_EXCEEDS_POLICY` rather than folded into a malformed-range error, and the refusal names the current limit so a reader can pick a period that will work.
- The period control offers only what the policy allows: the limit travels to the console with the site. If even the shortest period is over the limit the control keeps it, so the reader sees the refusal and its reason instead of an empty control.
- Measured the wider periods added in v0.27.0 against two million events. They are not slower — the ninety day overview is faster than the thirty day one, because the dominant cost is whole-history work rather than the window. Nothing exceeded the budget, so the ranges shipped last release are safe.

## v0.27.0

- Nine analytical screens gained a period control. The range was written into the request and could not be changed, so asking what happened this week was impossible on the frustration screen and the retention grid was fixed at six months. Experience comparison, retention, adoption, frustration, search analytics, feature intelligence, workspace roll-up, AI analytics and ecommerce now choose their own period.
- That also completes the recovery added in v0.26.0. The advice for a query that runs out of time starts with narrowing the range, and the slowest screen in the product was one where the range could not be narrowed; the button now appears there because there is something for it to do.
- Each screen offers the periods that suit it rather than one list everywhere: retention over 90, 180 and 365 days because cohorts are measured in months, feature intelligence over 30, 60 and 90, the rest over 7, 30 and 90. Insights and data quality stay at seven days — they report the current state rather than a period.
- The period control stays visible while the query runs and while an error is shown, so the range can be changed without leaving the screen.
- Renamed the retention grid's "기간" field to "표시 주차": it selects how many weeks the grid shows, which is a different thing from the period being analysed, and having both on screen made the old label ambiguous.

## v0.26.0

- A failed query now offers the recovery instead of describing it. The server already answered a timeout with advice — narrow the range, use a segment, run it in Fast mode, have it delivered on a schedule — and the console printed that as text under a generic "요청을 완료하지 못했습니다", leaving the reader to find where any of those live. Each step is now a button that goes there.
- The advice is only offered where it can be taken. A screen with a fixed range does not get a "narrow the range" button, and neither does a reader already on the shortest one; a permission error gets no buttons at all, because nothing on that screen would fix it.
- Cancelled, failed and rejected queries are told apart. A cancelled query says that leaving the screen cancels it, a failed one keeps the server's message for reporting, and being over the segment comparison limit says why the limit exists.
- Waiting says what is happening. After eight seconds the loading state reports that the query is taking longer than usual, and after twenty that it is approaching the limit — while there is still time to act rather than after the failure.
- `APIError` no longer uses constructor parameter properties, which the test runner cannot parse, so error-handling logic can be covered by tests.

## v0.25.1

- Added a scale guard that CI runs on every push. Two severe defects shipped because every test ran against a few hundred rows, where a query that scans the site per person and one that reads the table in visitor order both finish instantly. Fifty thousand events seed in two seconds and make the first class unmissable: with the aggregate that shipped before v0.24.1 restored, every segment-carrying report answers 504 and the query builder runs for nearly eight minutes on that data.
- Measured what the guard does not catch rather than assuming it catches everything. The rebuild defect fixed in v0.25.0 was a plan choice, not a query shape: with the old statement restored the rebuild finishes in three seconds at fifty thousand events and five at a quarter of a million, because the table still fits in cache. That class needs the two million event harness, and the test file says so.
- Guarded the other half of that fix directly: a test asserts the rebuild refreshes the statistics of the tables it fills, since a later step joins them and a latency budget cannot see the difference at any size CI can afford.

## v0.25.0

- Fixed a rebuild query that did not finish. Deriving visitor identities joined raw events back to a per-visitor subquery, and the planner answered that by reading the whole table in visitor order through an index — millions of random heap fetches. On a two million event site it was still running after thirty-three minutes; three grouped scans and two hash joins produce the same rows in under two seconds. An integration test runs both forms against the same data and compares every link, timestamp included.
- That rebuild runs inside the privacy deletion request, in the same transaction as the delete. On a site of any size the request could not complete, so the deletion it was part of was rolled back: deleting a person's data was not merely slow, it did not finish. The same query backs the full rebuild job, the timezone change and the retention sweep, and it holds a transaction open while it runs — a run left blocked an unrelated statement for ten minutes.
- The rebuild refreshes the planner's statistics before it starts and after each step a later step reads. It empties a table, fills it with hundreds of thousands of rows, and the next step joins it while the planner still believes it holds three — which turned a join of two small tables into a nested loop that ran for five minutes. A database user that may not analyse a table still rebuilds, on whatever statistics exist.
- The load harness now rebuilds the daily rollups the way the aggregation worker does before measuring. Reporting latency against empty rollups measured a path a running deployment rarely takes.

## v0.24.3

- The funnel and retention comparisons run their cohorts together rather than one after another. Comparing three segments used to mean four full evaluations in sequence; the funnel comparison went from 9.1 to 7.3 seconds at the median on a two million event site.
- The load harness now reports the median of three runs with the fastest and slowest alongside it, and checks the budget against the median. A single timing is not evidence: the same probe varied by three seconds between runs on an idle machine, which was enough to make one endpoint look improved and another look regressed when neither had changed.
- Removed the unused request parameter from the funnel evaluator, which never read it.

## v0.24.2

- Added a load harness that seeds two million events and times every analytical endpoint through the real router, failing any report that exceeds a 15 second budget. Correctness tests run against a few hundred rows, where a query that scans the site per person still finishes; this answers the question they cannot.
- Visitor insights was returning a timeout instead of a report. Its visitor bucket query read each person's first-ever activity with a scalar subquery evaluated once per person, against a scan of the site's entire history — the same shape that made behavioural segments unusable. Grouping once and joining is the same answer in one pass: the endpoint went from exceeding the 25 second deadline to 9.6 seconds.
- Every first-seen scan now stops at the end of the period being measured. A row after it cannot move anybody's first event into the window, so reading the rest made these queries grow with the site's whole history rather than with the period.
- The overview no longer waits for the sum of its three reads, and its period aggregate selects the columns it uses rather than every column including both jsonb blobs: 11.9 to 8.1 seconds.
- The experience report runs its four base reads concurrently, and the baseline and per-segment cohorts concurrently rather than one after another: 17.7 to 10.8 seconds with a segment, 6.9 to 3.9 without.

## v0.24.1

- Behavioural segments now run at scale. Every `entity.*` field compiled to an aggregate evaluated once per candidate row, and because `analytics_events.entity_id` is derived from a join with the identity table it cannot be indexed, so each evaluation scanned the site. On a two million event site a thirty day query did not finish inside a minute — past the analytical deadline, which means these segments returned a timeout rather than an answer on any site with real history. The same condition now compiles to a semi-join against one grouped subquery: 2.3 seconds on the same data, and an integration test runs both forms against the same rows to show they select the same people.
- The behavioural aggregate is scoped by the request's site and environment instead of by the outer row. A resolver built without them refuses to compile the aggregate rather than silently measuring the wrong population.
- The Frustration report runs its four independent reads concurrently. Measured serially on the same two million event site they came to roughly 9.6 seconds; the endpoint now waits for the slowest rather than the sum.
- `RunParallel` is exported from the insight package rather than reimplemented, so the connection-pool ceiling stays in one place.

## v0.24.0

- The Frustration report now says whether a signal costs anything. For every signal it compares the people who hit it against the people who did not and returns a verdict — conversion loss, no difference, occurs alongside conversion, or withheld — with the gap in points and an estimate of the conversions that gap accounts for.
- Signals are ranked by estimated lost conversions rather than by the size of the gap or the number of events. A modest gap that most people hit can be worth more than a severe gap almost nobody hits, and the ranking is the part that tells a reader where to start.
- Judgement is withheld unless both sides have at least twenty people. A signal almost everyone hits is withheld too: the handful who avoided it cannot be a baseline.
- A signal that fires on the way to converting — a retried form on the last step of a purchase — is reported as occurring alongside conversion instead of as harm, so nobody is sent to fix it.
- The comparison states that it is an association, not a cause, on the response itself rather than leaving the reader to assume.
- `analyze_frustration` returns the same impact analysis and caveat, so an agent asking about friction gets the ranking rather than raw counts, and no longer carries its own copy of the signal list.

## v0.23.0

- Friction and search are now expressible as audiences. Five behavioural segment fields join the existing ones: `entity.frustration_signals`, `entity.frustration_sessions`, `entity.searches`, `entity.zero_result_searches` and `entity.search_clicks`. "Hit friction twice and never converted" and "searched and found nothing" are now segment definitions, which means they can go straight into the funnel, retention and experience comparisons that already accept segments.
- The Frustration and Search reports hand over the audience instead of naming it. Each offers ready-to-save definitions — people the product blocked, people repeatedly blocked, people whose search returned nothing, people who searched repeatedly and opened nothing — with the count that the report itself measured, so saving a segment cannot disagree with the number the reader just saw.
- The Frustration table links each signal to the people who hit it. `?q=` on the user explorer accepts a search from any report, so a signal leads to real visitors and their timelines rather than to a count.
- The signal list behind the friction fields lives in one place shared by the report, the audiences and the segment aggregates, so they cannot drift apart.
- The audience list is one component now, used by visitor insights and both new reports instead of three copies of the same block.

- Fixed a test that asked the reports for "today" in the runner's timezone while every analytical endpoint answers in the site's. The fixture site is on Asia/Seoul, so a UTC afternoon is already the next day there and an event ingested during the test fell outside the window it was queried with — the v0.22.0 signal test failed in CI for that reason and passed locally. Every integration test now derives its dates from the site calendar.

## v0.22.0

- The tracker now detects the frustration signals the Frustration report has always scored. Rage clicks (three clicks on one element inside a second), dead clicks (a clickable-looking element that changed nothing), rapid backs, form retries from a resubmit or a failed validation, errors within two seconds of a click, and interactions slower than 500ms are all reported without any instrumentation. Seven of the report's nine signals previously arrived only from hand-written code, so the page was empty for every deployment using the tracker as shipped.
- Site search is detected from the query string of the results page, so search counts, click-through and refinements appear without instrumentation. `analytics.trackSearch(query, resultCount)` covers applications that search without changing the URL, and `data-momento-search-results` lets a page publish how many results it rendered, which is what makes the zero-result rate trustworthy. `data-momento-search-position` records which result was opened.
- Search terms stay out of the payload unless the site asks for them with `data-collect-search-terms="true"`, matching how button text is treated. A collected term is normalised, truncated to 100 characters, and stripped of email addresses, phone numbers and resident registration numbers in the browser before the server's own PII policy sees it.
- The events the tracker emits are no longer treated as unregistered by a strict event contract. Turning on reject mode previously required transcribing every built-in event, and shipping a new automatic signal would have dropped whole batches for the sites that had. A site that does register one keeps its own schema and validation mode.
- The Frustration report explains each signal and what to check, and both it and Search Analytics now distinguish "nothing is wrong" from "nothing is measuring": an empty report says which setting or snippet version to look at, and a search table with no terms says the term collection is deliberately off.
- Per-page signal reporting is capped at twenty so a page stuck in a render loop cannot turn into thousands of events.
- `tracker.js` grew from 15.2KB to 23.0KB minified. The detection code ships whether or not the signals are enabled.

## v0.21.4

- Verified privacy deletion end to end against a real database. Deleting by SSO user removes every row across raw events, sessions, visitors, visitor sessions, identity links, identified users and both daily rollups, while another person's data on the same site is untouched. Visitor, period and property deletion keep what is outside their boundary, and property deletion strips the key rather than the event.
- Verified that an export request cannot be downloaded before it is approved, that retention honours the per-site policy in both directions, and that a full aggregate rebuild finishes with no failed job.
- Separated a malformed request body from an invalid value in the privacy decision endpoint, which previously answered "decision must be approve or reject" for a body that carried an extra field.
- Added `Worker.ApplyRetention` and `Maintenance.RunPending` so retention and the aggregate queue can be driven once, by a test or by an operator applying a policy change now.

## v0.21.3

- Fixed the scheduled report kinds `visitor_insight` and `anomaly`, which the API accepted and the database rejected. The check constraint had never learned the kinds added in v0.10 and v0.11, so neither delivery could be created at all: the anomaly alert introduced in v0.11 and the form built for it in v0.19 both targeted a value the database refused.
- Added a regression test that reads the live check constraints and fails when a value the service considers valid would be rejected, covering report kinds, channel types and delivery statuses.
- Added write-path integration coverage: anomaly alert state through new, unchanged, opted-in, recovered and reopened transitions; secret issue, reveal, rotate, reveal again and re-seal; and scheduled delivery producing success, skipped and failed outcomes with the sealed credential actually sent and never listed back.
- Verified that a behavioural segment compiles and evaluates against real data, matching nobody when nobody qualifies rather than silently matching everyone.

## v0.21.2

- Fixed the query cost policy screen, which answered 500 for a site with no stored policy while the query guard silently applied defaults for the same site. Both now read one definition of the defaults, and the response says whether the values are stored or default.
- Extended the integration suite to the two largest unverified surfaces: the collector and worker ingestion path, and every advertised MCP tool. The collector test asserts that a blocked property, a blocked user property and a URL query string never reach storage, and that a wrong tracking key is refused.
- Added coverage for the remaining reports and governance endpoints: path, catalog, lineage, query audit, aggregate jobs, annotations, environments, contracts, semantic metrics and their evaluation, goals, journeys and workspace journeys with analysis, adoption targets, flags, experiments, privacy requests, delivery channels and runs, retention, the tracking debugger, audit, settings, users, networks, encryption status, contract CI validation, and the CSV and NDJSON exports.

## v0.21.1

- Fixed anomaly detection, which had been failing since v0.11: the daily error series aliased a column as `day`, a keyword PostgreSQL rejects in that position, so the endpoint answered 500 and the `anomaly` scheduled report failed every run. The v0.14 rollup change avoided the same mistake for the other four metrics but still reached this query for errors.
- Added an integration suite that runs the analytical endpoints against a real PostgreSQL instance: visitor insights, anomalies, all six attribution models in both scopes, cohort and experience comparison, visitor trace and search, funnel comparison, diagnostics, goals and sixteen reports. It asserts the reports actually compute rather than only that the SQL parses.
- Added a PostgreSQL service to CI so hand-written SQL is executed on every push. The suite skips when `MOMENTO_TEST_POSTGRES_DSN` is unset, so local unit runs are unchanged.

## v0.21.0

- Replaced the delivery channel headers JSON field with name and value rows, finishing what the scheduled report form started: a channel usually needs one credential, and asking for a JSON document turned a two field task into a syntax exercise.
- Reported the input the collector would reject before the request is sent: a duplicated header name, a Host override, a line break, or a name with no value.
- Restructured chapter 3 of the user guide, which eight feature releases had left with a duplicated 2.4, sections nested four levels deep, and the three comparison features scattered across unrelated numbers. Sections now follow the order a reader needs and the document has a table of contents.
- Corrected the user guide version, which had been left at v0.8.0 while the rest of the documentation moved.

## v0.20.0

- Ran the eight independent reads behind the visitor insight report concurrently with a ceiling of four, so the page waits for the slowest few queries instead of the sum of all of them, while one request still cannot exhaust the twenty connection pool.
- Moved the derived arithmetic out of the queries: channel and device shares are computed once every read has returned, rather than being threaded through a query that needed the visitor total.
- Returned the first failure and cancelled the remaining reads, because a partial report presented as a complete one is worse than an error.
- Treated an already cancelled request as cancelled rather than as an empty successful report.

## v0.19.0

- Replaced the hand-written definition document in the scheduled report form with kind-aware inputs: choosing what to send now shows only the values that kind actually uses, and the definition is built from them.
- Gave the anomaly alert its own inputs, which are notification states and an always-send switch rather than an aggregation range, and left the range to the reports that measure a period.
- Added a one-line summary of what will be delivered and how often before the schedule is saved.
- Added deep links from the anomaly card and the visitor insight header into the schedule form with the kind preselected, so a finding turns into a recurring delivery without hunting for the right screen.
- Kept a raw JSON escape hatch for unusual definitions, and stopped writing keys a report kind ignores, which previously read as configuration that did something.

## v0.18.0

- Added cohort comparison to the experience report: Core Web Vitals p75 and error exposure per segment, because a site-wide p75 averages a fast desktop on the office network together with a phone over VPN and hides both.
- Separated "slower" from "no longer acceptable" by checking the published Core Web Vitals thresholds: a cohort that crosses the bar while the baseline stays inside it is reported as critical rather than as a warning.
- Reported only differences worth acting on: at least 30 percent slower, or at least five points more error exposure, and never from fewer than twenty measurements.
- Stated plainly when no cohort is materially worse, instead of leaving an empty panel.

## v0.17.0

- Added cross-service conversion credit: with the workspace scope, a visit on a sibling service in the same workspace can earn credit for a conversion on this one, which is how a notice on one internal system leads to a submission on another.
- Reported credit per originating service alongside the channel breakdown, marking the service where the conversion happened.
- Restricted cross-service credit to people the identity graph knows, because an anonymous visitor is deliberately site scoped and matching them across services would be a guess.
- Widened the scope only to services the reader can already open, using the same access rule as the site list.
- Replaced the attribution parameter list with a query struct so the scope, lookback, model and half life travel together.

## v0.16.0

- Added an attention band to the Overview landing screen that merges detected anomalies and metric goals forecast to miss into one severity-ranked list, each with its evidence, next action and a link to the detail screen.
- Ranked an anomaly above a goal at the same severity, because an anomaly is a change that just happened while a goal is a standing target.
- Left achieved, on-track and not-yet-judged goals out, along with normal and insufficient-history anomalies, so the list itself carries a signal; an empty list states plainly that nothing is wrong.
- Kept the landing screen fast by reading only the rollup-based anomaly report and the metric registry, never the heavy insight report.
- Surfaced the alert state on the landing screen, so a problem reads as newly detected or open for a number of days.

## v0.15.0

- Added segment comparison to retention: up to three segments produce size-weighted average retention curves beside the baseline, with the same cohort definition applied to both who enters a cohort and what counts as a return.
- Excluded cohorts that are not yet old enough to have reached a period from that period's denominator, so a cohort started last week no longer drags a week-four number toward zero.
- Weighted the pooled curve by cohort size rather than averaging rates, which stops a three person cohort from outvoting a thousand person one.
- Named the first-return gap and the period where a segment falls furthest behind, in day, week or month units to match the selected granularity.
- Withheld a verdict for cohorts under twenty people and labelled them as an insufficient sample.

## v0.14.0

- Bounded every interactive analytical read at 25 seconds and cancelled the running statement with it, so one very wide range can no longer hold a database connection until the browser gives up.
- Reported a timeout as a 504 with the reason and the alternatives (narrow the range, apply a segment, schedule the report) instead of an internal error, and separated a client disconnect and a server-side cancel from a genuine failure.
- Built the anomaly baseline from the daily rollups the worker already maintains, falling back to the event table only for a day that has not been aggregated yet; this removes an eight week event scan from every insights page load and makes the numbers match the Overview screen.
- Added the `sessions(site_id, environment, started_at)` index that attribution touch lookups and the landing page report depend on, and pg_trgm indexes for visitor search that degrade to a sequential scan when the extension cannot be created.
- Left the event table without a new index on purpose: a non-concurrent build there would block ingestion during the startup migration.

## v0.13.0

- Added segment comparison to the funnel: up to three segments run beside the baseline with identical steps, mode and conversion window, so a flat overall completion rate becomes a per-cohort comparison.
- Named the step where each cohort loses the most ground against the baseline, and left it empty rather than inventing one when a cohort never falls behind.
- Charted the comparison on completion rate instead of user counts, because putting a small department next to a large one hides the shape of the funnel.
- Withheld a verdict for cohorts with fewer than twenty entrants and labelled them as an insufficient sample instead of reporting noise as a finding.
- Preserved the single-cohort funnel response, so existing callers of `POST /api/v1/funnel` are unaffected; `series` and `comparison` appear only when `compare_segment_ids` is sent.

## v0.12.1

- Bumped the pinned Go toolchain and the builder image to 1.26.7, clearing six standard library advisories (net/http, encoding/xml, encoding/asn1, golang.org/x/net idna) that were fixed in 1.26.6.

## v0.12.0

- Added multi-touch attribution with linear, time-decay (configurable half life) and position-based 40/20/40 models, expressed as weights over one shared path numbering so single-touch and multi-touch models never diverge in definition.
- Made attribution credit fractional: a conversion reached through three visits now contributes a third to each channel instead of naming one winner, and every model's weights sum to exactly one per conversion.
- Added average path length, touched conversions and touch share so the difference between models is visible rather than implied.
- Added anomaly alert state: a detection is reported as new, ongoing for a stated number of days, or recovered, so an hourly schedule stops re-announcing the same drop and announces its recovery once.
- Restricted anomaly delivery to new and recovered transitions by default, with `notify_on` to opt into ongoing alerts and same-day duplicate suppression; reading the report never rewrites alert history because only the delivery path persists state.
- Preserved the v0.11.0 SDK, collector, tracking protocol and privacy contracts. Migration `012_anomaly_state.sql` only adds the alert state table.
- Changed `credited_conversions` and the attribution totals from integers to numbers so fractional credit is representable; single-touch models still return whole numbers.

## v0.11.0

- Rebuilt the visitor timeline as a person-level trace: every visitor ID the deterministic identity graph links to one SSO user is merged into a single chronology, grouped into sessions with entry and exit pages, channel, device, engagement and the gap between consecutive events.
- Added the moment an anonymous visit became a known person as an explicit marker, plus a per-device identity link list showing when each browser profile joined the person.
- Added visitor search by SSO user id, department, organization, visitor id fragment, page URL, event name or feature, with an activity summary per candidate and trace deep links from the session and visitor reports.
- Added cursor paging so a long history can be walked backwards instead of silently stopping at the newest events, cross-service activity for the same SSO user, Markdown export of the trace, and an audit record for every individual-level lookup.
- Added anomaly detection that compares the last complete day against the same weekday median of the previous eight weeks using a median absolute deviation, so weekday seasonality and one-off outages no longer produce false alarms; thin history is reported as unjudged rather than guessed.
- Added the `anomaly` scheduled report kind that delivers only when something is detected, with `skipped` recorded as a normal delivery outcome instead of a failure.
- Added behavioural segment fields (`entity.sessions`, `entity.events`, `entity.conversions`, `entity.days_since_last_seen`, `entity.days_since_first_seen`) and one-click segment creation from every actionable audience in Visitor Insights.
- Added conversion attribution over session-level touchpoints with first-touch, last-touch and last-non-direct models, assisted conversions, assist-only credit and explicit unattributed conversions.
- Added metric goal landing forecasts with elapsed period share, projected value, required daily pace and an on-track verdict; rate metrics are not extrapolated and forecasts are withheld below ten percent of the period.
- Added the `detect_anomalies` and `analyze_attribution` MCP tools, bringing the analytics MCP surface to 22 tools.
- Preserved the v0.10.0 SDK, collector, tracking protocol and privacy contracts. Migration `011_anomaly_alerting.sql` only widens the delivery outcome constraint.

## v0.10.0

- Added a Visitor Insights report that pairs every visitor metric with the previous equivalent period and states the conclusion first: ranked findings that each carry their evidence, a likely cause, and the next action.
- Added default channel grouping over source and medium, including the internal portal, notice and messenger channels an on-premise employee deployment needs, plus a distinct `Direct (사내망)` group for corporate-network visits without acquisition data.
- Added new-versus-returning lifecycle structure, visit frequency and recency distributions, landing page bounce and conversion analysis, and device conversion gap detection.
- Added actionable audiences with counts and recommended next steps: repeat visitors who never convert, first-time visitors who never return, users active only in the previous period, and users returning from dormancy.
- Added one-click takeaway of the whole report as Markdown through clipboard copy and file download, with per-table CSV export retained.
- Added the `visitor_insight` scheduled report kind so the same report is delivered to webhook, mail, Confluence, internal messaging and AI agent channels, and the `get_visitor_insights` MCP tool so an agent can pull it directly.
- Added a goal-aware comparison so metrics whose direction is ambiguous, such as the share of first-time visitors, are no longer coloured as progress or regression.
- Extracted the report into `internal/insight` so the console, MCP surface and scheduled delivery share one narrative, and covered the classification, thresholds and digest rendering with unit tests.
- Preserved the v0.9.0 database, SDK, collector, tracking protocol, REST/OpenAPI and privacy contracts; no migration and no new environment variable.

## v0.9.0

- Added `MOMENTO_ENCRYPTION_KEY` (with the shared `ENCRYPTION_KEY` alias) so personal API keys, site tracking keys, server API keys, OIDC client secrets, and delivery channel headers are stored with AES-256-GCM and survive a restart instead of being lost or re-entered.
- Added key re-display for sites and personal API keys with audit logging, replacing rotation as the only way to recover a lost key.
- Added `MOMENTO_ENCRYPTION_KEY_PREVIOUS` rotation support, an encryption status endpoint, and an administrator re-seal action that finishes a key rotation without a redeploy.
- Fixed collector requests being blocked by the measured application's Content-Security-Policy by shipping the exact `script-src`/`connect-src` policy, a meta tag, and a reverse proxy snippet in the console, the tracking-code API, and the documentation.
- Added SDK `data-endpoint` support for a first-party collector proxy and a CSP violation listener that names the required policy in the browser console.
- Made the console Content-Security-Policy configurable through the public URL origin and a new `additional_connect_origins` security setting, and stopped sending a document policy on collector responses.
- Added a server-side install diagnostics report and console tab covering site state, ingestion volume, CSP guidance, allowed domains, environment match, pipeline backlog, and key recoverability.
- Added console access to previously server-only capabilities: session report, raw event export, delivery run history, delivery channel and schedule deletion, event contract activation and CI validation, semantic metric evaluation, and workspace business journeys.
- Added regression tests for the encryption keyring, environment configuration, CSP construction and guidance, origin allowlist matching, and SDK endpoint resolution.
- Preserved the v0.8.0 database, SDK, collector, tracking protocol, REST/OpenAPI, and privacy contracts; the three required environment variables are unchanged and encryption stays optional.

## v0.8.0

- Fixed Path Analysis rendering failures when real journeys contained bidirectional or cyclic transitions by projecting origins and destinations into separate acyclic graph layers.
- Kept Sankey nodes and links under the same top-transition limit, preventing links from referencing omitted nodes, and added a contextual empty state plus movement summaries.
- Added an operational briefing to Administration with a readiness score, seven-day collection and quality metrics, pending workflow counts, and manual refresh.
- Added a severity-ordered action queue for collector failures, dead letters, failed aggregate jobs, pending privacy requests, data quality degradation, unrestricted origins, administrator redundancy, SSO, and inactive SDK collection.
- Added readiness checks for collection boundaries, URL privacy, value-based PII policy, administrator redundancy, and Enterprise SSO with direct remediation links.
- Added recent administrator activity to the control plane and shareable deep links for Analytics Engineering and Product Lab panels.
- Added a first-class PII value-detection policy editor with server-side validation for `detect`, `warn`, `mask`, and `reject`.
- Added browser-console regression tests for cyclic Path data and graph node/link consistency, and included them in local and CI verification.
- Preserved the v0.7.0 database, SDK, collector, tracking protocol, REST/OpenAPI, privacy, and three-environment-variable runtime contracts.

## v0.7.0

- Reorganized the console into role-aware, collapsible navigation groups for monitoring, web, product, exploration, experience, and AI analytics.
- Added a global `Ctrl/Cmd+K` command palette for pages, analytics functions, and permission-aware administration shortcuts.
- Added persistent breadcrumbs, site/environment context, responsive mobile navigation, and clearer active-location cues.
- Rebuilt Administration as a task-oriented control center with a summary home, grouped sticky navigation, mobile selector, and shareable `?section=` deep links.
- Split Analytics Engineering into Metric/Goal, Query Cost, Aggregate, Change Calendar, and Catalog/Lineage workspaces; split Product Lab into Feature Flag and Experiment workspaces.
- Upgraded shared data tables with search, pagination, result counts, responsive overflow, and UTF-8 CSV export.
- Replaced generic loading and empty states with contextual skeletons, retry guidance, and setup actions.
- Added explicit confirmation UX for full aggregate rebuilds and privacy request decisions, plus clearer required-field and mutation feedback.
- Refined the MUI theme, focus visibility, cards, dialogs, tooltips, reduced-motion behavior, and responsive spacing for an accessible enterprise console.
- Preserved all v0.6.0 API, database, tracking protocol, SDK, privacy, and three-environment-variable runtime contracts.

## v0.6.0

- Added Formula-capable Semantic Metrics with metric references, user/session/event property filters, minimum occurrence rules, owners, scopes, tags, and shared evaluation across REST, Explorer, Goals, and MCP.
- Added occurrence-time Session Property snapshots across SDK, Collector, Raw Events, materialized Sessions, PII filtering, Semantic Metric session scope, and deterministic Raw Event rebuilds.
- Added Metric Goals with target/comparator/period/environment/organization/department ownership and live achievement evaluation.
- Added deterministic Exact/Fast/Preview query modes, complexity scoring, sampling policy, cost rejection, execution metadata, and Query Audit history.
- Added Event Contract CI validation, Event Catalog usage health, Data Lineage from Event to Metric to Goal, and explicit data ownership metadata.
- Added value-based PII detection before the durable inbox with `detect`, `warn`, `mask`, and `reject` policies. Detector samples never retain matching secrets.
- Added Late Event detection and deduplicated automatic date-range aggregate rebuild jobs, plus administrator-requested date-range and full rebuilds.
- Added Cardinality health levels (`low`, `medium`, `high`, `extreme`) and Query Builder eligibility guidance.
- Added Workspace Roll-Up and cross-site Business Journeys using deterministic SSO identity while keeping anonymous Visitors site-scoped.
- Added Service Score, Feature Score, adoption/repeat/conversion/error signals, trend comparison, and Dead Feature candidates.
- Added first-class Search Analytics for Zero Result, CTR, refinement, exit and success; and privacy-preserving Frustration Analytics for rage/dead clicks, retries, errors, and slow interactions.
- Added Feature Flag and Experiment registries with Variant population, Semantic primary metrics, Lift, and two-proportion confidence estimates.
- Added Change Calendar annotations for deployment, release, incident, campaign, training, feature flag and organization changes.
- Added audited Privacy Request workflow for delete/export authorization with separate request and approval steps and complete NDJSON downloads.
- Expanded Analytics MCP from 13 to 19 tools with Workspace Roll-Up, Feature Score, Search, Frustration, Metric Goals, and Event Catalog access.
- Added dedicated React workspaces for Enterprise Analytics, Analytics Engineering, Product Lab, and Privacy Requests.
- Verified both clean PostgreSQL 17 installation and in-place v0.5.0 to v0.6.0 migration while retaining exactly three required runtime environment variables.

## v0.5.0

- Added first-class DEV/STG/PRD and custom environment isolation across the SDK, Collector, Raw Events, daily aggregates, core reports, Funnel, Path, Export, Query API and new analytics workspaces.
- Added immutable Event Contract versions with draft/active/deprecated lifecycle and environment-specific `allow`, `warn`, or `reject` enforcement.
- Added a safe AST-based Semantic Metric Registry with definition versioning, built-in metrics, REST evaluation, Query API support and MCP access.
- Added the Data Quality Center with Tracking Health Score, inbox lag, duplicate/late/rejected events, contract warnings, PII-block counts, missing dimensions, dead letters and daily cardinality guards.
- Added weekly/daily/monthly Cohort and Retention analysis with configurable cohort and return events.
- Added reusable 2-12 step Business Journeys with sequential matching, conversion windows, overall conversion and average elapsed time.
- Added Organization/Department Feature Adoption with eligible-user targets, repeat usage, active and dormant users.
- Added automatic SDK RUM for LCP, INP, CLS, FCP, TTFB, page load and resource errors plus Error Conversion Impact and Release Impact reports.
- Added offline rule-based Insight/Root Cause detection and an offline Korean/English natural-language analytics parser with no external data transfer.
- Added standardized AI/Model/Agent/MCP/Tool analytics for calls, users, success rate, latency, tokens, cost and fallback behavior.
- Added allowlisted Scheduled Reports and Segment actions for Webhook, Confluence, Mail gateway, internal messaging and AI Agent endpoints, with write-only header values and delivery audit history.
- Expanded Analytics MCP from 5 to 13 tools for Semantic Metrics, Retention, Adoption, Experience, AI operations, Data Quality and offline questions.
- Added SDK environment-scoped Visitor/Session/Offline storage and release context fields (`app_version`, `release_version`, `git_sha`, `deployment_id`).
- Preserved v0.4 browser identity continuity by migrating legacy production Visitor, Session, Consent and Offline Queue storage into site-scoped keys.
- Kept the runtime contract at exactly three required environment variables and retained PostgreSQL Raw Events as the immutable source of truth.

## v0.4.0

- Added a deterministic Visitor Identity Graph that links anonymous and authenticated Visitor IDs through pseudonymous `user_id` values without fingerprinting.
- Added canonical User profiles so historical anonymous events inherit the identified user's latest User-scope department, organization, and custom dimensions while Raw Events remain immutable.
- Applied canonical identity to Overview, Realtime, Events, Pages, internal usage, Query Builder, Segment event-existence rules, Funnel, Ecommerce, Session reports, and Analytics MCP.
- Added `visitors`, `identified_users`, `visitor_identities`, `visitor_sessions`, `daily_site_metrics`, `daily_site_visitors`, and `daily_site_sessions` PostgreSQL summaries with automatic v0.3 Raw Event backfill.
- Added an `analytics_events` read model with collision-safe `u:`/`v:` entity identifiers and canonical user properties.
- Added the privacy-controlled Identity Graph REST report, User Explorer graph UI, linked-Visitor navigation, and `query_identity_graph` MCP tool.
- Accelerated Site-local Overview trends with daily aggregates while retaining exact Raw Event fallback for partial-day ranges.
- Rebuilt daily aggregates atomically when a Site timezone changes.
- Expanded User ID privacy deletion to every linked anonymous/device Visitor and atomically rebuilt all affected derived data.

## v0.3.0

- Captured page, device, and first-touch acquisition context at event occurrence time so SPA route changes cannot rewrite queued event context.
- Made `consent-required` fail closed even when browser storage is unavailable, while preserving an in-memory consent grant and the original acquisition context.
- Changed automatic DOM text collection to opt-in, sanitized common PII patterns in browser error messages, and removed query strings and fragments from SDK URLs by default.
- Changed the server privacy baseline to strip URL query strings before the durable inbox write.
- Added user and session conversion counts/rates; retained `conversion_rate` as a compatibility alias for user conversion rate.
- Defined engaged sessions as threshold duration, a conversion, two page views, or sufficient active engagement, and materialized active milliseconds, heartbeat count, and interaction count.
- Added per-site IANA timezone and engagement-threshold settings with an administrator editor and immediate Session summary reconciliation.
- Applied site-local calendar boundaries to dashboard, reports, Query, Funnel, exports, privacy deletion, and MCP date ranges.
- Isolated Worker inbox jobs with PostgreSQL savepoints so a failed event cannot abort retry/dead-letter bookkeeping or block valid jobs in the same batch.
- Made privacy deletion atomically scrub Inbox/Dead Letter payloads, update Raw Events, and rebuild Session summaries.

## v0.2.0

- Added nested AND/OR Segment Registry with personal and shared ownership.
- Applied saved or inline segments to Query Builder and Funnel analysis.
- Added open/closed Funnel modes, per-step property conditions, and completion windows.
- Added saved Exploration definitions and Custom Dimension Registry.
- Added Ecommerce summary, product performance, and commerce funnel reports.
- Added privacy-controlled Visitor Timeline and materialized Session reports.
- Added per-site retention policies and migration-time Session backfill.
- Extended Analytics MCP with Ecommerce and Segment tools.

## v0.1.0

- Initial on-premise event analytics MVP.
- PostgreSQL durable inbox and Raw Event storage, JavaScript SDK, React console, Keycloak OIDC, RBAC, privacy, REST API, and MCP.
- Offline Docker image release workflow.
