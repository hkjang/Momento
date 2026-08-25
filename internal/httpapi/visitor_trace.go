package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/hkjang/Momento/internal/auth"
	"github.com/hkjang/Momento/internal/insight"
)

// Tracing a real visitor means following one person, not one browser profile. A
// person signs in on a desktop and a phone, so the trace merges every visitor ID
// the deterministic identity graph links to the same SSO user, groups the events
// into sessions, and shows when each device joined the person.

const timelineEventLimit = 200

func (s *Server) visitorProfilesEnabled(ctx context.Context) bool {
	var enabled bool
	if err := s.DB.QueryRow(ctx, `SELECT coalesce((value->>'visitor_profiles')::boolean,true) FROM settings WHERE key='privacy'`).Scan(&enabled); err != nil {
		return false
	}
	return enabled
}

type traceSubject struct {
	VisitorID      string
	UserID         string
	Scope          string
	VisitorIDs     []string
	UserProperties any
}

// resolveTraceSubject accepts a visitor ID or an SSO user ID and returns every
// visitor ID that belongs to the same person. Scope "device" keeps the trace on
// the single browser profile that was asked for.
func (s *Server) resolveTraceSubject(ctx context.Context, siteID uuid.UUID, value, scope string) (traceSubject, error) {
	subject := traceSubject{VisitorID: value, Scope: "device", VisitorIDs: []string{value}}
	var canonical *string
	// The value is a visitor ID when the identity graph or the visitor summary knows it.
	if err := s.DB.QueryRow(ctx, `SELECT user_id FROM visitor_identities WHERE site_id=$1 AND visitor_id=$2`, siteID, value).Scan(&canonical); err != nil {
		var exists bool
		_ = s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM visitors WHERE site_id=$1 AND visitor_id=$2)`, siteID, value).Scan(&exists)
		if !exists {
			// Fall back to treating the value as an SSO user ID.
			var userExists bool
			_ = s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM identified_users WHERE site_id=$1 AND user_id=$2)`, siteID, value).Scan(&userExists)
			if !userExists {
				return subject, nil
			}
			subject.VisitorID = ""
			canonical = &value
		}
	}
	if canonical == nil || *canonical == "" {
		return subject, nil
	}
	subject.UserID = *canonical
	var properties []byte
	if s.DB.QueryRow(ctx, `SELECT user_properties FROM identified_users WHERE site_id=$1 AND user_id=$2`, siteID, subject.UserID).Scan(&properties) == nil {
		_ = json.Unmarshal(properties, &subject.UserProperties)
	}
	if scope == "device" && subject.VisitorID != "" {
		return subject, nil
	}
	rows, err := s.DB.Query(ctx, `SELECT visitor_id FROM visitor_identities WHERE site_id=$1 AND user_id=$2 ORDER BY first_seen`, siteID, subject.UserID)
	if err != nil {
		return subject, err
	}
	defer rows.Close()
	linked := []string{}
	for rows.Next() {
		var visitorID string
		if rows.Scan(&visitorID) == nil {
			linked = append(linked, visitorID)
		}
	}
	if len(linked) == 0 {
		return subject, rows.Err()
	}
	subject.Scope = "person"
	subject.VisitorIDs = linked
	if subject.VisitorID == "" {
		subject.VisitorID = linked[len(linked)-1]
	}
	return subject, rows.Err()
}

type traceEvent struct {
	EventID              string         `json:"event_id"`
	EventName            string         `json:"event_name"`
	Timestamp            time.Time      `json:"timestamp"`
	VisitorID            string         `json:"visitor_id"`
	SessionID            string         `json:"session_id"`
	UserID               *string        `json:"user_id"`
	PageURL              *string        `json:"page_url"`
	PageTitle            *string        `json:"page_title"`
	Referrer             *string        `json:"referrer"`
	IsConversion         bool           `json:"is_conversion"`
	Properties           any            `json:"properties"`
	SecondsSincePrevious float64        `json:"seconds_since_previous"`
	Marker               string         `json:"marker,omitempty"`
	Environment          string         `json:"environment"`
	ContractVersion      int            `json:"contract_version"`
	TrafficClass         string         `json:"traffic_class"`
	context              map[string]any `json:"-"`
}

type traceSession struct {
	SessionID          string       `json:"session_id"`
	VisitorID          string       `json:"visitor_id"`
	StartedAt          time.Time    `json:"started_at"`
	EndedAt            time.Time    `json:"ended_at"`
	DurationSeconds    float64      `json:"duration_seconds"`
	Events             []traceEvent `json:"events"`
	EventCount         int          `json:"event_count"`
	PageViews          int          `json:"page_views"`
	Conversions        int          `json:"conversions"`
	Engaged            bool         `json:"engaged"`
	Partial            bool         `json:"partial"`
	DeviceType         string       `json:"device_type"`
	Browser            string       `json:"browser"`
	OS                 string       `json:"os"`
	Source             string       `json:"source"`
	Medium             string       `json:"medium"`
	Campaign           string       `json:"campaign"`
	Network            string       `json:"network"`
	LandingPage        string       `json:"landing_page"`
	ExitPage           string       `json:"exit_page"`
	InteractionCount   int64        `json:"interaction_count"`
	ActiveEngagementMS int64        `json:"active_engagement_ms"`
}

// visitorTimeline returns a session-grouped trace of one person or one device.
func (s *Server) visitorTimeline(w http.ResponseWriter, r *http.Request) {
	preventCaching(w)
	if !s.visitorProfilesEnabled(r.Context()) {
		writeError(w, 403, "VISITOR_PROFILES_DISABLED", "Visitor Explorer is disabled by the privacy policy")
		return
	}
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	value := strings.TrimSpace(chi.URLParam(r, "visitorID"))
	if value == "" || len(value) > 128 {
		writeError(w, 400, "INVALID_VISITOR", "visitor or user id is required")
		return
	}
	from, to, err := s.dateRange(r, siteID)
	if err != nil {
		writeError(w, 400, "INVALID_RANGE", err.Error())
		return
	}
	environment := requestEnvironment(r)
	scope := r.URL.Query().Get("scope")
	if scope != "device" {
		scope = "person"
	}
	subject, err := s.resolveTraceSubject(r.Context(), siteID, value, scope)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = timelineEventLimit
	}
	// The cursor walks backwards through history so a long trace can be followed
	// page by page instead of silently stopping at the newest events.
	before := to
	if raw := strings.TrimSpace(r.URL.Query().Get("before")); raw != "" {
		parsed, parseErr := time.Parse(time.RFC3339, raw)
		if parseErr != nil {
			writeError(w, 400, "INVALID_CURSOR", "before must be an RFC3339 timestamp")
			return
		}
		if parsed.Before(before) {
			before = parsed
		}
	}
	ctx, cancel := s.analyticalContext(r)
	defer cancel()
	events, hasMore, err := s.traceEvents(ctx, siteID, environment, subject.VisitorIDs, from, before, limit)
	if err != nil {
		writeQueryError(w, err)
		return
	}
	sessions, err := s.groupTraceSessions(ctx, siteID, environment, events)
	if err != nil {
		writeQueryError(w, err)
		return
	}
	summary, err := s.traceSummary(ctx, siteID, environment, subject.VisitorIDs)
	if err != nil {
		writeQueryError(w, err)
		return
	}
	links, err := s.traceIdentityLinks(ctx, siteID, subject.UserID)
	if err != nil {
		writeQueryError(w, err)
		return
	}
	otherSites, err := s.traceOtherSites(ctx, siteID, subject.UserID)
	if err != nil {
		writeQueryError(w, err)
		return
	}
	nextBefore := ""
	if hasMore && len(events) > 0 {
		nextBefore = events[len(events)-1].Timestamp.UTC().Format(time.RFC3339Nano)
	}
	p, _ := auth.FromContext(r.Context())
	// Looking at one person's activity is an individual-level lookup, so it is audited.
	s.audit(r.Context(), &p, "visitor.timeline.view", "visitor", subject.VisitorID, map[string]any{"user_id": subject.UserID, "scope": subject.Scope, "visitors": len(subject.VisitorIDs)}, clientIP(r))
	writeJSON(w, 200, map[string]any{
		"visitor_id":         subject.VisitorID,
		"user_id":            nullableUser(subject.UserID),
		"scope":              subject.Scope,
		"visitor_ids":        subject.VisitorIDs,
		"linked_visitor_ids": subject.VisitorIDs,
		"user_properties":    subject.UserProperties,
		"summary":            summary,
		"identity_links":     links,
		"other_sites":        otherSites,
		"sessions":           sessions,
		"window":             map[string]any{"from": from, "to": to, "environment": environment},
		"paging":             map[string]any{"limit": limit, "has_more": hasMore, "next_before": nextBefore},
	})
}

func nullableUser(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// traceEvents reads one page of events for every visitor ID of the subject. The
// per-visitor index keeps this cheap even on a large event table.
func (s *Server) traceEvents(ctx context.Context, siteID uuid.UUID, environment string, visitorIDs []string, from, before time.Time, limit int) ([]traceEvent, bool, error) {
	rows, err := s.DB.Query(ctx, `SELECT event_id,event_name,event_timestamp,visitor_id,session_id,user_id,page_url,page_title,referrer,properties,is_conversion,traffic_class,environment,contract_version,device_type,browser,os,source,medium,campaign,network_name
		FROM raw_events WHERE site_id=$1 AND visitor_id = ANY($2) AND environment=$3 AND event_timestamp >= $4 AND event_timestamp < $5
		ORDER BY event_timestamp DESC,id DESC LIMIT $6`, siteID, visitorIDs, environment, from, before, limit+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	events := []traceEvent{}
	for rows.Next() {
		var event traceEvent
		var eventID uuid.UUID
		var properties []byte
		var device, browser, os, source, medium, campaign, network *string
		if rows.Scan(&eventID, &event.EventName, &event.Timestamp, &event.VisitorID, &event.SessionID, &event.UserID, &event.PageURL, &event.PageTitle, &event.Referrer,
			&properties, &event.IsConversion, &event.TrafficClass, &event.Environment, &event.ContractVersion, &device, &browser, &os, &source, &medium, &campaign, &network) != nil {
			continue
		}
		event.EventID = eventID.String()
		_ = json.Unmarshal(properties, &event.Properties)
		event.context = map[string]any{"device_type": text(device), "browser": text(browser), "os": text(os), "source": text(source), "medium": text(medium), "campaign": text(campaign), "network": text(network)}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	hasMore := len(events) > limit
	if hasMore {
		events = events[:limit]
	}
	return events, hasMore, nil
}

func text(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// groupTraceSessions turns the flat page of events into visits. A visit is what a
// person actually did in one sitting, so the trace reads as a story instead of a log.
func (s *Server) groupTraceSessions(ctx context.Context, siteID uuid.UUID, environment string, events []traceEvent) ([]traceSession, error) {
	if len(events) == 0 {
		return []traceSession{}, nil
	}
	grouped := map[string]*traceSession{}
	order := []string{}
	for index := len(events) - 1; index >= 0; index-- {
		event := events[index]
		key := event.VisitorID + "|" + event.SessionID
		session, ok := grouped[key]
		if !ok {
			session = &traceSession{SessionID: event.SessionID, VisitorID: event.VisitorID, StartedAt: event.Timestamp}
			if context := event.context; context != nil {
				session.DeviceType, _ = context["device_type"].(string)
				session.Browser, _ = context["browser"].(string)
				session.OS, _ = context["os"].(string)
				session.Source, _ = context["source"].(string)
				session.Medium, _ = context["medium"].(string)
				session.Campaign, _ = context["campaign"].(string)
				session.Network, _ = context["network"].(string)
			}
			grouped[key] = session
			order = append(order, key)
		}
		if len(session.Events) > 0 {
			previous := session.Events[len(session.Events)-1]
			event.SecondsSincePrevious = event.Timestamp.Sub(previous.Timestamp).Seconds()
		}
		if event.EventName == "page_view" {
			session.PageViews++
			if session.LandingPage == "" {
				session.LandingPage = text(event.PageURL)
			}
			session.ExitPage = text(event.PageURL)
		}
		if event.IsConversion {
			session.Conversions++
		}
		session.EndedAt = event.Timestamp
		session.Events = append(session.Events, event)
	}
	sessionIDs := make([]string, 0, len(order))
	sessions := make([]traceSession, 0, len(order))
	for _, key := range order {
		sessionIDs = append(sessionIDs, grouped[key].SessionID)
	}
	// Enrich with the materialized session so engagement follows the site rule and
	// a session that started before this page is marked as partial.
	stored := map[string]struct {
		Started, Ended    time.Time
		Events, PageViews int64
		Conversions       int64
		Engaged           bool
		Landing, Exit     *string
		Interactions      int64
		ActiveMS          int64
	}{}
	rows, err := s.DB.Query(ctx, `SELECT session_id,started_at,last_event_at,event_count,page_views,conversion_count,engaged,landing_page,exit_page,interaction_count,active_engagement_ms
		FROM sessions WHERE site_id=$1 AND environment=$2 AND session_id = ANY($3)`, siteID, environment, sessionIDs)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		var value struct {
			Started, Ended    time.Time
			Events, PageViews int64
			Conversions       int64
			Engaged           bool
			Landing, Exit     *string
			Interactions      int64
			ActiveMS          int64
		}
		if rows.Scan(&id, &value.Started, &value.Ended, &value.Events, &value.PageViews, &value.Conversions, &value.Engaged, &value.Landing, &value.Exit, &value.Interactions, &value.ActiveMS) == nil {
			stored[id] = value
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, key := range order {
		session := grouped[key]
		session.EventCount = len(session.Events)
		if summary, ok := stored[session.SessionID]; ok {
			session.Engaged = summary.Engaged
			session.InteractionCount = summary.Interactions
			session.ActiveEngagementMS = summary.ActiveMS
			session.Partial = int64(session.EventCount) < summary.Events
			if summary.Landing != nil && *summary.Landing != "" {
				session.LandingPage = *summary.Landing
			}
			if summary.Exit != nil && *summary.Exit != "" {
				session.ExitPage = *summary.Exit
			}
			if summary.Started.Before(session.StartedAt) {
				session.StartedAt = summary.Started
			}
			if summary.Ended.After(session.EndedAt) {
				session.EndedAt = summary.Ended
			}
		}
		session.DurationSeconds = session.EndedAt.Sub(session.StartedAt).Seconds()
		markIdentifyMoment(session)
		sessions = append(sessions, *session)
	}
	sort.SliceStable(sessions, func(i, j int) bool { return sessions[i].StartedAt.After(sessions[j].StartedAt) })
	return sessions, nil
}

// markIdentifyMoment labels the event where an anonymous visit became a known
// person, which is the moment that makes the rest of the trace attributable.
func markIdentifyMoment(session *traceSession) {
	seenUser := false
	for index := range session.Events {
		hasUser := session.Events[index].UserID != nil && *session.Events[index].UserID != ""
		if hasUser && !seenUser {
			session.Events[index].Marker = "identified"
			seenUser = true
		}
	}
}

func (s *Server) traceSummary(ctx context.Context, siteID uuid.UUID, environment string, visitorIDs []string) (map[string]any, error) {
	summary := map[string]any{}
	var firstSeen, lastSeen *time.Time
	var events, sessions, conversions, pageViews, activeDays int64
	err := s.DB.QueryRow(ctx, `SELECT min(event_timestamp),max(event_timestamp),count(*),count(DISTINCT session_id),count(*) FILTER(WHERE is_conversion),count(*) FILTER(WHERE event_name='page_view'),count(DISTINCT date_trunc('day',event_timestamp))
		FROM raw_events WHERE site_id=$1 AND visitor_id = ANY($2) AND environment=$3`, siteID, visitorIDs, environment).
		Scan(&firstSeen, &lastSeen, &events, &sessions, &conversions, &pageViews, &activeDays)
	if err != nil {
		return nil, err
	}
	summary["first_seen"] = firstSeen
	summary["last_seen"] = lastSeen
	summary["events"] = events
	summary["sessions"] = sessions
	summary["conversions"] = conversions
	summary["page_views"] = pageViews
	summary["active_days"] = activeDays
	top := func(expression string, key string, limit int) error {
		rows, err := s.DB.Query(ctx, `SELECT `+expression+` value,count(*) events,max(event_timestamp) last_seen
			FROM raw_events WHERE site_id=$1 AND visitor_id = ANY($2) AND environment=$3 AND `+expression+` IS NOT NULL AND `+expression+` <> ''
			GROUP BY 1 ORDER BY 2 DESC LIMIT `+strconv.Itoa(limit), siteID, visitorIDs, environment)
		if err != nil {
			return err
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var value string
			var count int64
			var last time.Time
			if rows.Scan(&value, &count, &last) == nil {
				out = append(out, map[string]any{"value": value, "events": count, "last_seen": last})
			}
		}
		summary[key] = out
		return rows.Err()
	}
	if err := top("page_url", "top_pages", 8); err != nil {
		return nil, err
	}
	if err := top("properties->>'feature'", "top_features", 8); err != nil {
		return nil, err
	}
	if err := top("device_type", "devices", 5); err != nil {
		return nil, err
	}
	if err := top("network_name", "networks", 5); err != nil {
		return nil, err
	}
	return summary, nil
}

// traceIdentityLinks shows when each browser profile joined the person, so an
// analyst can tell device switching apart from a shared account.
func (s *Server) traceIdentityLinks(ctx context.Context, siteID uuid.UUID, userID string) ([]map[string]any, error) {
	out := []map[string]any{}
	if userID == "" {
		return out, nil
	}
	rows, err := s.DB.Query(ctx, `SELECT visitor_id,first_seen,linked_at,last_seen,source,confidence FROM visitor_identities WHERE site_id=$1 AND user_id=$2 ORDER BY linked_at`, siteID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var visitorID, source string
		var first, linked, last time.Time
		var confidence float64
		if rows.Scan(&visitorID, &first, &linked, &last, &source, &confidence) == nil {
			out = append(out, map[string]any{"visitor_id": visitorID, "first_seen": first, "linked_at": linked, "last_seen": last, "source": source, "confidence": confidence})
		}
	}
	return out, rows.Err()
}

// traceOtherSites reports the same SSO user on sibling services in the workspace.
// It reads the identified user summary, never the event tables, so it stays cheap.
func (s *Server) traceOtherSites(ctx context.Context, siteID uuid.UUID, userID string) ([]map[string]any, error) {
	out := []map[string]any{}
	if userID == "" {
		return out, nil
	}
	rows, err := s.DB.Query(ctx, `SELECT s.site_key,s.name,u.first_seen,u.last_seen
		FROM identified_users u JOIN sites s ON s.id=u.site_id
		WHERE u.user_id=$2 AND s.id<>$1 AND s.workspace_id=(SELECT workspace_id FROM sites WHERE id=$1)
		ORDER BY u.last_seen DESC LIMIT 20`, siteID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var siteKey, name string
		var first, last time.Time
		if rows.Scan(&siteKey, &name, &first, &last) == nil {
			out = append(out, map[string]any{"site_id": siteKey, "name": name, "first_seen": first, "last_seen": last})
		}
	}
	return out, rows.Err()
}

// visitorSearch finds a real person from what an analyst actually knows: an SSO
// user ID, a department, a page they were on, or an event they triggered.
func (s *Server) visitorSearch(w http.ResponseWriter, r *http.Request) {
	preventCaching(w)
	if !s.visitorProfilesEnabled(r.Context()) {
		writeError(w, 403, "VISITOR_PROFILES_DISABLED", "Visitor Explorer is disabled by the privacy policy")
		return
	}
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(query) < 2 || len(query) > 128 {
		writeError(w, 400, "INVALID_QUERY", "q must be between 2 and 128 characters")
		return
	}
	from, to, err := s.dateRange(r, siteID)
	if err != nil {
		writeError(w, 400, "INVALID_RANGE", err.Error())
		return
	}
	environment := requestEnvironment(r)
	ctx, cancel := s.analyticalContext(r)
	defer cancel()
	pattern := "%" + strings.ToLower(query) + "%"
	results := map[string]map[string]any{}
	order := []string{}
	addRow := func(visitorID, userID, matchedBy, matchedValue string) {
		if visitorID == "" {
			return
		}
		if existing, ok := results[visitorID]; ok {
			if existing["user_id"] == nil && userID != "" {
				existing["user_id"] = userID
			}
			return
		}
		results[visitorID] = map[string]any{"visitor_id": visitorID, "user_id": nullableUser(userID), "matched_by": matchedBy, "matched_value": matchedValue}
		order = append(order, visitorID)
	}

	// 1. SSO user ID, department or organization from the identified user summary.
	identityRows, err := s.DB.Query(ctx, `SELECT u.user_id,coalesce(u.user_properties->>'department',''),coalesce(u.user_properties->>'organization',''),i.visitor_id
		FROM identified_users u JOIN visitor_identities i ON i.site_id=u.site_id AND i.user_id=u.user_id
		WHERE u.site_id=$1 AND (lower(u.user_id) LIKE $2 OR lower(coalesce(u.user_properties->>'department','')) LIKE $2 OR lower(coalesce(u.user_properties->>'organization','')) LIKE $2)
		ORDER BY u.last_seen DESC LIMIT 60`, siteID, pattern)
	if err != nil {
		writeQueryError(w, err)
		return
	}
	for identityRows.Next() {
		var userID, department, organization, visitorID string
		if identityRows.Scan(&userID, &department, &organization, &visitorID) != nil {
			continue
		}
		matchedBy, matchedValue := "user_id", userID
		lowered := strings.ToLower(query)
		if !strings.Contains(strings.ToLower(userID), lowered) {
			if strings.Contains(strings.ToLower(department), lowered) {
				matchedBy, matchedValue = "department", department
			} else if strings.Contains(strings.ToLower(organization), lowered) {
				matchedBy, matchedValue = "organization", organization
			}
		}
		addRow(visitorID, userID, matchedBy, matchedValue)
	}
	identityRows.Close()

	// 2. Visitor ID fragment, for tracing from a support ticket or a log line.
	visitorRows, err := s.DB.Query(ctx, `SELECT v.visitor_id,coalesce(v.user_id,'') FROM visitors v WHERE v.site_id=$1 AND lower(v.visitor_id) LIKE $2 ORDER BY v.last_seen DESC LIMIT 30`, siteID, pattern)
	if err != nil {
		writeQueryError(w, err)
		return
	}
	for visitorRows.Next() {
		var visitorID, userID string
		if visitorRows.Scan(&visitorID, &userID) == nil {
			addRow(visitorID, userID, "visitor_id", visitorID)
		}
	}
	visitorRows.Close()

	// 3. A page or an event within the selected window, bounded by the site and
	// time index so the lookup stays predictable.
	activityRows, err := s.DB.Query(ctx, `SELECT DISTINCT ON (visitor_id) visitor_id,coalesce(user_id,''),event_name,coalesce(page_url,'')
		FROM raw_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3
		AND (lower(event_name) LIKE $5 OR lower(coalesce(page_url,'')) LIKE $5 OR lower(coalesce(properties->>'feature','')) LIKE $5)
		ORDER BY visitor_id,event_timestamp DESC LIMIT 60`, siteID, from, to, environment, pattern)
	if err != nil {
		writeQueryError(w, err)
		return
	}
	for activityRows.Next() {
		var visitorID, userID, eventName, pageURL string
		if activityRows.Scan(&visitorID, &userID, &eventName, &pageURL) != nil {
			continue
		}
		matchedBy, matchedValue := "event", eventName
		if strings.Contains(strings.ToLower(pageURL), strings.ToLower(query)) {
			matchedBy, matchedValue = "page", pageURL
		}
		addRow(visitorID, userID, matchedBy, matchedValue)
	}
	activityRows.Close()

	if len(order) == 0 {
		writeJSON(w, 200, map[string]any{"query": query, "results": []map[string]any{}})
		return
	}
	// Attach the activity summary so a searcher can pick the right person.
	ids := make([]string, 0, len(order))
	ids = append(ids, order...)
	summaryRows, err := s.DB.Query(ctx, `SELECT v.visitor_id,v.first_seen,v.last_seen,v.event_count,v.conversion_count,
		(SELECT count(*) FROM visitor_sessions vs WHERE vs.site_id=v.site_id AND vs.visitor_id=v.visitor_id)
		FROM visitors v WHERE v.site_id=$1 AND v.visitor_id = ANY($2)`, siteID, ids)
	if err != nil {
		writeQueryError(w, err)
		return
	}
	for summaryRows.Next() {
		var visitorID string
		var first, last time.Time
		var events, conversions, sessions int64
		if summaryRows.Scan(&visitorID, &first, &last, &events, &conversions, &sessions) != nil {
			continue
		}
		if row, ok := results[visitorID]; ok {
			row["first_seen"], row["last_seen"] = first, last
			row["events"], row["conversions"], row["sessions"] = events, conversions, sessions
		}
	}
	summaryRows.Close()

	out := make([]map[string]any, 0, len(order))
	for _, visitorID := range order {
		out = append(out, results[visitorID])
	}
	sort.SliceStable(out, func(i, j int) bool {
		left, leftOK := out[i]["last_seen"].(time.Time)
		right, rightOK := out[j]["last_seen"].(time.Time)
		if leftOK && rightOK {
			return left.After(right)
		}
		return leftOK && !rightOK
	})
	if len(out) > 25 {
		out = out[:25]
	}
	writeJSON(w, 200, map[string]any{"query": query, "environment": environment, "results": out})
}

// siteAnomalies evaluates the anomaly watch list for the last complete day.
func (s *Server) siteAnomalies(w http.ResponseWriter, r *http.Request) {
	preventCaching(w)
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	_, location, err := s.siteTimezone(r.Context(), siteID)
	if err != nil {
		writeError(w, 500, "INVALID_TIMEZONE", err.Error())
		return
	}
	environment := requestEnvironment(r)
	ctx, cancel := s.analyticalContext(r)
	defer cancel()
	reporter := insight.New(s.DB)
	report, err := reporter.DetectSiteAnomalies(ctx, siteID, environment, location)
	if err != nil {
		writeQueryError(w, err)
		return
	}
	// Reading the report never rewrites alert history; only delivery does that.
	states, err := reporter.AnomalyStates(ctx, siteID, environment)
	if err != nil {
		writeQueryError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{
		"environment":    report.Environment,
		"evaluated_date": report.EvaluatedDate,
		"timezone":       report.Timezone,
		"baseline_weeks": report.BaselineWeeks,
		"detected":       report.Detected,
		"checked":        report.Checked,
		"note":           report.Note,
		"transitions":    insight.AnnotateStates(report, states),
		"notify_on":      insight.NotifiableStates(),
	})
}

// channelAttribution credits conversions to channel groups using the requested model.
func (s *Server) channelAttribution(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	from, to, err := s.dateRange(r, siteID)
	if err != nil {
		writeError(w, 400, "INVALID_RANGE", err.Error())
		return
	}
	model := strings.TrimSpace(r.URL.Query().Get("model"))
	if model == "" {
		model = "last_non_direct"
	}
	if _, ok := insight.AttributionOrder(model); !ok {
		writeError(w, 400, "INVALID_MODEL", "unsupported attribution model")
		return
	}
	lookback, _ := strconv.Atoi(r.URL.Query().Get("lookback_days"))
	halfLife, _ := strconv.Atoi(r.URL.Query().Get("half_life_days"))
	ctx, cancel := s.analyticalContext(r)
	defer cancel()
	report, err := insight.New(s.DB).Attribution(ctx, siteID, requestEnvironment(r), from, to, lookback, model, halfLife)
	if err != nil {
		writeQueryError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"report": report, "models": insight.AttributionModels()})
}
