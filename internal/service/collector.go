package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hkjang/Momento/internal/auth"
	"github.com/hkjang/Momento/internal/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var eventNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`)
var knownBotPattern = regexp.MustCompile(`(?i)(bot\b|crawler|spider|slurp|bingpreview|headlesschrome)`)
var monitoringPattern = regexp.MustCompile(`(?i)(uptime|pingdom|healthcheck|monitoring|prometheus)`)

type SiteForCollect struct {
	ID             uuid.UUID
	SiteKey        string
	TrackingHash   string
	ServerKeyHash  string
	AllowedDomains []string
	MaxEvents      int
	PrivacyRaw     []byte
}

type CollectorService struct{ DB *pgxpool.Pool }

func (s CollectorService) Accept(ctx context.Context, req model.CollectRequest, origin, clientIP, userAgent string) (int64, error) {
	if req.SiteID == "" || req.VisitorID == "" || req.SessionID == "" || len(req.Events) == 0 {
		return 0, errors.New("site_id, visitor_id, session_id and events are required")
	}
	if len(req.VisitorID) > 128 || len(req.SessionID) > 128 || len(req.UserID) > 128 {
		return 0, errors.New("identifier is too long")
	}
	for _, event := range req.Events {
		if !eventNamePattern.MatchString(event.Name) {
			return 0, fmt.Errorf("invalid event name %q", event.Name)
		}
		if len(event.Properties) > 100 {
			return 0, fmt.Errorf("event %q has too many properties", event.Name)
		}
	}
	var site SiteForCollect
	err := s.DB.QueryRow(ctx, `SELECT id,site_key,tracking_key_hash,server_api_key_hash,allowed_domains,least(1000,greatest(1,coalesce((SELECT (value->>'max_events_per_request')::int FROM settings WHERE key='security'),100))),(SELECT value FROM settings WHERE key='privacy') FROM sites WHERE site_key=$1 AND active`, req.SiteID).
		Scan(&site.ID, &site.SiteKey, &site.TrackingHash, &site.ServerKeyHash, &site.AllowedDomains, &site.MaxEvents, &site.PrivacyRaw)
	if err != nil {
		return 0, errors.New("unknown site")
	}
	providedHash := auth.HashToken(req.TrackingKey)
	if origin == "" {
		if req.TrackingKey == "" || providedHash != site.ServerKeyHash {
			return 0, errors.New("server API key is required for server-side events")
		}
	} else if req.TrackingKey != "" && providedHash != site.TrackingHash && providedHash != site.ServerKeyHash {
		return 0, errors.New("invalid tracking key")
	}
	if origin != "" && !originAllowed(origin, site.AllowedDomains) {
		return 0, errors.New("origin is not allowed")
	}
	if len(req.Events) > site.MaxEvents {
		return 0, fmt.Errorf("at most %d events are accepted per request", site.MaxEvents)
	}
	names := make([]string, 0, len(req.Events))
	for _, event := range req.Events {
		names = append(names, event.Name)
	}
	definitionRows, err := s.DB.Query(ctx, `SELECT name,validation_mode,schema FROM event_definitions WHERE site_id=$1 AND name=ANY($2)`, site.ID, names)
	if err != nil {
		return 0, err
	}
	type definition struct {
		mode   string
		schema []byte
	}
	definitions := map[string]definition{}
	for definitionRows.Next() {
		var name string
		var item definition
		if err := definitionRows.Scan(&name, &item.mode, &item.schema); err != nil {
			definitionRows.Close()
			return 0, err
		}
		definitions[name] = item
	}
	definitionRows.Close()
	for i := range req.Events {
		if item, ok := definitions[req.Events[i].Name]; ok && item.mode != "allow" {
			warnings := validateProperties(req.Events[i].Properties, item.schema)
			if len(warnings) > 0 && item.mode == "reject" {
				return 0, fmt.Errorf("schema validation failed for %s: %s", req.Events[i].Name, strings.Join(warnings, "; "))
			}
			if len(warnings) > 0 {
				if req.Events[i].Properties == nil {
					req.Events[i].Properties = map[string]any{}
				}
				req.Events[i].Properties["_momento_schema_warnings"] = warnings
			}
		}
	}
	var privacy privacyConfig
	_ = json.Unmarshal(site.PrivacyRaw, &privacy)
	applyPrivacyBeforeQueue(&req, &clientIP, &userAgent, privacy)
	payload := model.InboxPayload{Request: req, ClientIP: clientIP, Origin: origin, UserAgent: userAgent, ReceivedUnix: time.Now().UnixMilli()}
	body, err := payload.JSON()
	if err != nil {
		return 0, err
	}
	var inboxID int64
	err = s.DB.QueryRow(ctx, `INSERT INTO event_inbox(site_id,payload) VALUES($1,$2) RETURNING id`, site.ID, body).Scan(&inboxID)
	return inboxID, err
}

func originAllowed(origin string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	for _, domain := range allowed {
		domain = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(domain, ".")))
		if domain == host || (strings.HasPrefix(domain, "*.") && strings.HasSuffix(host, domain[1:])) {
			return true
		}
	}
	return false
}

type privacyConfig struct {
	IPAnonymization   bool     `json:"ip_anonymization"`
	CollectUserAgent  bool     `json:"collect_user_agent"`
	StripQueryString  bool     `json:"strip_query_string"`
	MaskedParameters  []string `json:"masked_parameters"`
	CollectUserID     bool     `json:"collect_user_id"`
	DoNotTrack        bool     `json:"do_not_track"`
	BlockedProperties []string `json:"blocked_properties"`
}

type Worker struct{ DB *pgxpool.Pool }

func (w Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(100 * time.Millisecond)
	cleanup := time.NewTicker(time.Hour)
	defer ticker.Stop()
	defer cleanup.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.processBatch(ctx); err != nil && ctx.Err() == nil {
				slog.Error("event worker batch failed", "error", err)
			}
		case <-cleanup.C:
			if err := w.cleanup(ctx); err != nil && ctx.Err() == nil {
				slog.Error("event worker cleanup failed", "error", err)
			}
		}
	}
}

func (w Worker) processBatch(ctx context.Context) error {
	tx, err := w.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT id,site_id,payload,attempts FROM event_inbox WHERE processed_at IS NULL AND available_at<=now() ORDER BY id LIMIT 100 FOR UPDATE SKIP LOCKED`)
	if err != nil {
		return err
	}
	type job struct {
		id       int64
		siteID   uuid.UUID
		payload  []byte
		attempts int
	}
	jobs := make([]job, 0, 100)
	for rows.Next() {
		var j job
		if err := rows.Scan(&j.id, &j.siteID, &j.payload, &j.attempts); err != nil {
			rows.Close()
			return err
		}
		jobs = append(jobs, j)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	var privacy privacyConfig
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT value FROM settings WHERE key='privacy'`).Scan(&raw); err == nil {
		_ = json.Unmarshal(raw, &privacy)
	}
	for _, job := range jobs {
		if _, err := tx.Exec(ctx, `SAVEPOINT momento_inbox_job`); err != nil {
			return err
		}
		if processErr := w.processOne(ctx, tx, job.siteID, job.payload, privacy); processErr != nil {
			// A PostgreSQL statement error aborts the current transaction until it is
			// rolled back. Isolating every inbox job keeps the retry/dead-letter update
			// writable and prevents one malformed event from blocking the whole batch.
			if _, err := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT momento_inbox_job`); err != nil {
				return fmt.Errorf("rollback failed inbox job %d: %w", job.id, err)
			}
			if job.attempts >= 9 {
				if _, err := tx.Exec(ctx, `INSERT INTO event_dead_letters(inbox_id,site_id,payload,error) VALUES($1,$2,$3,$4)`, job.id, job.siteID, job.payload, processErr.Error()); err != nil {
					return fmt.Errorf("dead-letter inbox job %d: %w", job.id, err)
				}
				if _, err := tx.Exec(ctx, `UPDATE event_inbox SET attempts=attempts+1,last_error=$2,processed_at=now() WHERE id=$1`, job.id, processErr.Error()); err != nil {
					return fmt.Errorf("finish dead-lettered inbox job %d: %w", job.id, err)
				}
			} else {
				if _, err := tx.Exec(ctx, `UPDATE event_inbox SET attempts=attempts+1,last_error=$2,available_at=now()+least(interval '5 minutes', interval '1 second' * power(2,least(attempts,8))) WHERE id=$1`, job.id, processErr.Error()); err != nil {
					return fmt.Errorf("schedule retry for inbox job %d: %w", job.id, err)
				}
			}
			if _, err := tx.Exec(ctx, `RELEASE SAVEPOINT momento_inbox_job`); err != nil {
				return err
			}
			continue
		}
		if _, err := tx.Exec(ctx, `RELEASE SAVEPOINT momento_inbox_job`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE event_inbox SET processed_at=now(),last_error=NULL WHERE id=$1`, job.id); err != nil {
			return fmt.Errorf("finish inbox job %d: %w", job.id, err)
		}
	}
	return tx.Commit(ctx)
}

func (w Worker) processOne(ctx context.Context, tx pgx.Tx, siteID uuid.UUID, body []byte, privacy privacyConfig) error {
	var p model.InboxPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return err
	}
	ip := net.ParseIP(strings.TrimSpace(p.ClientIP))
	if privacy.IPAnonymization {
		ip = anonymizeIP(ip)
	}
	networkName := "External / Unclassified"
	internal := false
	if ip != nil {
		_ = tx.QueryRow(ctx, `SELECT name,internal FROM network_ranges WHERE $1::inet <<= cidr ORDER BY masklen(cidr) DESC LIMIT 1`, ip.String()).Scan(&networkName, &internal)
	}
	userID := p.Request.UserID
	if !privacy.CollectUserID {
		userID = ""
	}
	userProps := filteredProperties(p.Request.UserProperties, privacy.BlockedProperties)
	userAgent := p.UserAgent
	if !privacy.CollectUserAgent {
		userAgent = ""
	}
	trafficClass := "normal"
	if strings.TrimSpace(p.UserAgent) == "" {
		trafficClass = "suspicious"
	} else if monitoringPattern.MatchString(p.UserAgent) {
		trafficClass = "monitoring"
	} else if knownBotPattern.MatchString(p.UserAgent) {
		trafficClass = "known_bot"
	}
	if internal {
		trafficClass = "internal_traffic"
	}
	for _, event := range p.Request.Events {
		ctxValue := p.Request.Context
		if event.Context != nil {
			ctxValue = *event.Context
		}
		props := filteredProperties(event.Properties, privacy.BlockedProperties)
		if len(event.Items) > 0 {
			items := make([]map[string]any, 0, len(event.Items))
			for _, item := range event.Items {
				items = append(items, filteredProperties(item, privacy.BlockedProperties))
			}
			props["items"] = items
		}
		pageURL := sanitizeURL(ctxValue.Page.URL, privacy)
		eventID, err := uuid.Parse(event.ID)
		if err != nil {
			eventID, err = uuid.NewV7()
			if err != nil {
				return err
			}
		}
		eventTime := time.UnixMilli(event.Timestamp)
		if event.Timestamp <= 0 || eventTime.Before(time.Now().AddDate(-5, 0, 0)) || eventTime.After(time.Now().Add(24*time.Hour)) {
			eventTime = time.Now()
		}
		conversion := event.Name == "conversion" || event.Name == "purchase"
		if !conversion {
			_ = tx.QueryRow(ctx, `SELECT conversion FROM event_definitions WHERE site_id=$1 AND name=$2`, siteID, event.Name).Scan(&conversion)
		}
		propsJSON, _ := json.Marshal(props)
		userJSON, _ := json.Marshal(userProps)
		result, err := tx.Exec(ctx, `INSERT INTO raw_events(event_id,site_id,event_name,event_timestamp,visitor_id,session_id,user_id,page_url,page_title,referrer,source,medium,campaign,device_type,browser,os,language,screen,user_agent,client_ip,network_name,properties,user_properties,is_conversion,is_internal,traffic_class)
			VALUES($1,$2,$3,$4,$5,$6,nullif($7,''),nullif($8,''),nullif($9,''),nullif($10,''),nullif($11,''),nullif($12,''),nullif($13,''),nullif($14,''),nullif($15,''),nullif($16,''),nullif($17,''),nullif($18,''),nullif($19,''),$20,$21,$22,$23,$24,$25,$26)
			ON CONFLICT(site_id,event_id) DO NOTHING`, eventID, siteID, event.Name, eventTime, p.Request.VisitorID, p.Request.SessionID, userID, pageURL, ctxValue.Page.Title, ctxValue.Page.Referrer, ctxValue.Traffic.Source, ctxValue.Traffic.Medium, ctxValue.Traffic.Campaign, ctxValue.Device.Type, ctxValue.Device.Browser, ctxValue.Device.OS, ctxValue.Device.Language, ctxValue.Device.Screen, userAgent, nullableIP(ip), networkName, propsJSON, userJSON, conversion, internal, trafficClass)
		if err != nil {
			return err
		}
		if result.RowsAffected() > 0 {
			if err := updateSession(ctx, tx, siteID, p.Request, event, eventTime, userID, pageURL, ctxValue, conversion); err != nil {
				return err
			}
		}
	}
	return nil
}

func updateSession(ctx context.Context, tx pgx.Tx, siteID uuid.UUID, request model.CollectRequest, event model.IncomingEvent, eventTime time.Time, userID, pageURL string, eventContext model.EventContext, conversion bool) error {
	var page any
	if event.Name == "page_view" && pageURL != "" {
		page = pageURL
	}
	activeMS := activeEngagementMilliseconds(event)
	heartbeat := boolInt(event.Name == "user_engagement")
	interaction := boolInt(isInteractionEvent(event.Name))
	_, err := tx.Exec(ctx, `INSERT INTO sessions(site_id,session_id,visitor_id,user_id,started_at,last_event_at,event_count,page_views,conversion_count,engaged,landing_page,exit_page,source,medium,campaign,device_type,active_engagement_ms,heartbeat_count,interaction_count)
		VALUES($1,$2,$3,nullif($4,''),$5,$5,1,$6::bigint,$7::bigint,($7::bigint>0 OR $6::bigint>=2 OR $13::bigint >= (SELECT engagement_threshold_seconds::bigint*1000 FROM sites WHERE id=$1)),$8,$8,nullif($9,''),nullif($10,''),nullif($11,''),nullif($12,''),$13::bigint,$14::bigint,$15::bigint)
		ON CONFLICT(site_id,session_id) DO UPDATE SET
		visitor_id=excluded.visitor_id,
		user_id=coalesce(excluded.user_id,sessions.user_id),
		landing_page=CASE WHEN excluded.started_at<sessions.started_at AND excluded.landing_page IS NOT NULL THEN excluded.landing_page ELSE coalesce(sessions.landing_page,excluded.landing_page) END,
		exit_page=CASE WHEN excluded.last_event_at>=sessions.last_event_at AND excluded.exit_page IS NOT NULL THEN excluded.exit_page ELSE sessions.exit_page END,
		started_at=least(sessions.started_at,excluded.started_at),
		last_event_at=greatest(sessions.last_event_at,excluded.last_event_at),
		event_count=sessions.event_count+1,
		page_views=sessions.page_views+excluded.page_views,
		conversion_count=sessions.conversion_count+excluded.conversion_count,
		active_engagement_ms=sessions.active_engagement_ms+excluded.active_engagement_ms,
		heartbeat_count=sessions.heartbeat_count+excluded.heartbeat_count,
		interaction_count=sessions.interaction_count+excluded.interaction_count,
		engaged=(extract(epoch FROM (greatest(sessions.last_event_at,excluded.last_event_at)-least(sessions.started_at,excluded.started_at))) >= (SELECT engagement_threshold_seconds FROM sites WHERE id=$1)
			OR sessions.conversion_count+excluded.conversion_count>0
			OR sessions.page_views+excluded.page_views>=2
			OR sessions.active_engagement_ms+excluded.active_engagement_ms >= (SELECT engagement_threshold_seconds*1000 FROM sites WHERE id=$1)),
		source=coalesce(sessions.source,excluded.source),
		medium=coalesce(sessions.medium,excluded.medium),
		campaign=coalesce(sessions.campaign,excluded.campaign),
		device_type=coalesce(sessions.device_type,excluded.device_type),
		updated_at=now()`, siteID, request.SessionID, request.VisitorID, userID, eventTime, boolInt(event.Name == "page_view"), boolInt(conversion), page, eventContext.Traffic.Source, eventContext.Traffic.Medium, eventContext.Traffic.Campaign, eventContext.Device.Type, activeMS, heartbeat, interaction)
	return err
}

func activeEngagementMilliseconds(event model.IncomingEvent) int64 {
	if event.Name != "user_engagement" {
		return 0
	}
	var seconds float64
	switch value := event.Properties["active_seconds"].(type) {
	case float64:
		seconds = value
	case float32:
		seconds = float64(value)
	case int:
		seconds = float64(value)
	case int64:
		seconds = float64(value)
	case json.Number:
		seconds, _ = value.Float64()
	case string:
		seconds, _ = strconv.ParseFloat(value, 64)
	}
	if seconds < 0 {
		return 0
	}
	if seconds > 3600 {
		seconds = 3600
	}
	return int64(seconds * 1000)
}

func isInteractionEvent(name string) bool {
	switch name {
	case "click", "outbound_click", "file_download", "search", "login", "sign_up", "form_start", "form_submit", "conversion", "purchase", "add_to_cart", "begin_checkout", "error":
		return true
	default:
		return false
	}
}

func boolInt(value bool) int64 {
	if value {
		return 1
	}
	return 0
}

func filteredProperties(in map[string]any, blocked []string) map[string]any {
	out := make(map[string]any, len(in))
	blockedSet := make(map[string]bool, len(blocked))
	for _, key := range blocked {
		blockedSet[strings.ToLower(key)] = true
	}
	for key, value := range in {
		if !blockedSet[strings.ToLower(key)] {
			out[key] = filterNested(value, blockedSet)
		}
	}
	return out
}
func applyPrivacyBeforeQueue(req *model.CollectRequest, clientIP, userAgent *string, privacy privacyConfig) {
	if !privacy.CollectUserID {
		req.UserID = ""
	}
	req.UserProperties = filteredProperties(req.UserProperties, privacy.BlockedProperties)
	req.Context.Page.URL = sanitizeURL(req.Context.Page.URL, privacy)
	req.Context.Page.Referrer = sanitizeURL(req.Context.Page.Referrer, privacy)
	for i := range req.Events {
		req.Events[i].Properties = filteredProperties(req.Events[i].Properties, privacy.BlockedProperties)
		items := make([]map[string]any, 0, len(req.Events[i].Items))
		for _, item := range req.Events[i].Items {
			items = append(items, filteredProperties(item, privacy.BlockedProperties))
		}
		req.Events[i].Items = items
		if req.Events[i].Context != nil {
			req.Events[i].Context.Page.URL = sanitizeURL(req.Events[i].Context.Page.URL, privacy)
			req.Events[i].Context.Page.Referrer = sanitizeURL(req.Events[i].Context.Page.Referrer, privacy)
		}
	}
	if privacy.IPAnonymization {
		if ip := anonymizeIP(net.ParseIP(*clientIP)); ip != nil {
			*clientIP = ip.String()
		} else {
			*clientIP = ""
		}
	}
	if !privacy.CollectUserAgent {
		*userAgent = ""
	}
}
func filterNested(value any, blocked map[string]bool) any {
	switch current := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, nested := range current {
			if !blocked[strings.ToLower(key)] {
				out[key] = filterNested(nested, blocked)
			}
		}
		return out
	case []any:
		out := make([]any, len(current))
		for i, nested := range current {
			out[i] = filterNested(nested, blocked)
		}
		return out
	default:
		return value
	}
}

func sanitizeURL(raw string, privacy privacyConfig) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	// URL fragments frequently contain client-side route state and access tokens.
	// They are never required for server-side page aggregation.
	u.Fragment = ""
	if privacy.StripQueryString {
		u.RawQuery = ""
		return u.String()
	}
	q := u.Query()
	for _, key := range privacy.MaskedParameters {
		if q.Has(key) {
			q.Set(key, "[MASKED]")
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func anonymizeIP(ip net.IP) net.IP {
	if ip == nil {
		return nil
	}
	if v4 := ip.To4(); v4 != nil {
		return net.IPv4(v4[0], v4[1], v4[2], 0)
	}
	v6 := ip.To16()
	if v6 == nil {
		return nil
	}
	out := append(net.IP(nil), v6...)
	for i := 8; i < 16; i++ {
		out[i] = 0
	}
	return out
}

func nullableIP(ip net.IP) any {
	if ip == nil {
		return nil
	}
	return ip.String()
}

func validateProperties(properties map[string]any, schemaRaw []byte) []string {
	var schema struct {
		Required   []string `json:"required"`
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
	}
	if json.Unmarshal(schemaRaw, &schema) != nil {
		return []string{"invalid registry schema"}
	}
	warnings := []string{}
	for _, key := range schema.Required {
		value, ok := properties[key]
		missing := !ok || value == nil
		if text, isString := value.(string); isString && text == "" {
			missing = true
		}
		if missing {
			warnings = append(warnings, key+" is required")
		}
	}
	for key, definition := range schema.Properties {
		value, ok := properties[key]
		if !ok || value == nil {
			continue
		}
		valid := true
		switch definition.Type {
		case "string":
			_, valid = value.(string)
		case "number", "integer":
			switch value.(type) {
			case float64, float32, int, int32, int64, json.Number:
				valid = true
			default:
				valid = false
			}
		case "boolean":
			_, valid = value.(bool)
		case "array":
			_, valid = value.([]any)
		case "object":
			_, valid = value.(map[string]any)
		}
		if !valid {
			warnings = append(warnings, key+" must be "+definition.Type)
		}
	}
	return warnings
}

func (w Worker) cleanup(ctx context.Context) error {
	var months, debugDays int
	if err := w.DB.QueryRow(ctx, `SELECT coalesce((value->>'raw_event_retention_months')::int,13),coalesce((value->>'debug_retention_days')::int,7) FROM settings WHERE key='privacy'`).Scan(&months, &debugDays); err != nil {
		return err
	}
	if months < 1 {
		months = 1
	}
	if months > 120 {
		months = 120
	}
	if debugDays < 1 {
		debugDays = 1
	}
	if debugDays > 90 {
		debugDays = 90
	}
	if _, err := w.DB.Exec(ctx, `DELETE FROM raw_events e WHERE e.event_timestamp < now()-make_interval(months=>coalesce((SELECT p.raw_event_months FROM retention_policies p WHERE p.site_id=e.site_id),$1))`, months); err != nil {
		return err
	}
	if _, err := w.DB.Exec(ctx, `DELETE FROM sessions s WHERE s.last_event_at < now()-make_interval(months=>coalesce((SELECT p.session_months FROM retention_policies p WHERE p.site_id=s.site_id),25))`); err != nil {
		return err
	}
	if _, err := w.DB.Exec(ctx, `DELETE FROM event_inbox i WHERE i.processed_at < now()-make_interval(days=>coalesce((SELECT p.debug_days FROM retention_policies p WHERE p.site_id=i.site_id),$1))`, debugDays); err != nil {
		return err
	}
	if _, err := w.DB.Exec(ctx, `DELETE FROM event_dead_letters d WHERE d.failed_at < now()-make_interval(days=>coalesce((SELECT p.debug_days FROM retention_policies p WHERE p.site_id=d.site_id),$1))`, debugDays); err != nil {
		return err
	}
	if _, err := w.DB.Exec(ctx, `DELETE FROM user_sessions WHERE expires_at<now()`); err != nil {
		return err
	}
	_, err := w.DB.Exec(ctx, `DELETE FROM oidc_states WHERE expires_at<now()`)
	return err
}

// ProcessPending is used by tests and operational drains.
func (w Worker) ProcessPending(ctx context.Context) error { return w.processBatch(ctx) }
