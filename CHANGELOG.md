# Changelog

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
