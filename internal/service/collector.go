package service

import (
	"context"
	"crypto/sha256"
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
var environmentPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
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
	Timezone       string
}

type CollectorService struct{ DB *pgxpool.Pool }

func (s CollectorService) Accept(ctx context.Context, req model.CollectRequest, origin, clientIP, userAgent string) (int64, error) {
	if req.Environment == "" {
		req.Environment = "prd"
	}
	if req.SiteID == "" || req.VisitorID == "" || req.SessionID == "" || len(req.Events) == 0 {
		return 0, errors.New("site_id, visitor_id, session_id and events are required")
	}
	if len(req.VisitorID) > 128 || len(req.SessionID) > 128 || len(req.UserID) > 128 {
		return 0, errors.New("identifier is too long")
	}
	if !environmentPattern.MatchString(req.Environment) {
		return 0, errors.New("invalid environment")
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
	err := s.DB.QueryRow(ctx, `SELECT id,site_key,tracking_key_hash,server_api_key_hash,allowed_domains,least(1000,greatest(1,coalesce((SELECT (value->>'max_events_per_request')::int FROM settings WHERE key='security'),100))),(SELECT value FROM settings WHERE key='privacy'),timezone FROM sites WHERE site_key=$1 AND active`, req.SiteID).
		Scan(&site.ID, &site.SiteKey, &site.TrackingHash, &site.ServerKeyHash, &site.AllowedDomains, &site.MaxEvents, &site.PrivacyRaw, &site.Timezone)
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
	var environmentMode string
	if err := s.DB.QueryRow(ctx, `SELECT contract_mode FROM site_environments WHERE site_id=$1 AND name=$2 AND active`, site.ID, req.Environment).Scan(&environmentMode); err != nil {
		return 0, errors.New("unknown or inactive environment")
	}
	names := make([]string, 0, len(req.Events))
	for _, event := range req.Events {
		names = append(names, event.Name)
	}
	definitionRows, err := s.DB.Query(ctx, `SELECT d.name,d.current_version,v.version,v.validation_mode,v.schema
		FROM event_definitions d JOIN event_contract_versions v ON v.site_id=d.site_id AND v.event_name=d.name
		WHERE d.site_id=$1 AND d.name=ANY($2) AND v.status<>'draft'`, site.ID, names)
	if err != nil {
		return 0, err
	}
	type definition struct {
		mode   string
		schema []byte
	}
	type eventContracts struct {
		current  int
		versions map[int]definition
	}
	definitions := map[string]eventContracts{}
	for definitionRows.Next() {
		var name string
		var current, version int
		var item definition
		if err := definitionRows.Scan(&name, &current, &version, &item.mode, &item.schema); err != nil {
			definitionRows.Close()
			return 0, err
		}
		contracts := definitions[name]
		if contracts.versions == nil {
			contracts = eventContracts{current: current, versions: map[int]definition{}}
		}
		contracts.versions[version] = item
		definitions[name] = contracts
	}
	definitionRows.Close()
	for i := range req.Events {
		contracts, known := definitions[req.Events[i].Name]
		warnings := []string{}
		if !known {
			warnings = append(warnings, "event is not registered")
		} else {
			if req.Events[i].ContractVersion == 0 {
				req.Events[i].ContractVersion = contracts.current
			}
			item, exists := contracts.versions[req.Events[i].ContractVersion]
			if !exists {
				warnings = append(warnings, fmt.Sprintf("contract version %d is not registered", req.Events[i].ContractVersion))
			} else if strictestValidationMode(environmentMode, item.mode) != "allow" {
				warnings = append(warnings, validateProperties(req.Events[i].Properties, item.schema)...)
			}
		}
		if req.Events[i].ContractVersion == 0 {
			req.Events[i].ContractVersion = 1
		}
		if len(warnings) > 0 {
			mode := environmentMode
			if contracts, ok := definitions[req.Events[i].Name]; ok {
				if item, ok := contracts.versions[req.Events[i].ContractVersion]; ok {
					mode = strictestValidationMode(mode, item.mode)
				}
			}
			if mode == "reject" {
				s.recordQualityRejection(ctx, site, req.Environment, req.Events[i], warnings)
				return 0, fmt.Errorf("schema validation failed for %s: %s", req.Events[i].Name, strings.Join(warnings, "; "))
			}
			if mode == "warn" {
				if req.Events[i].Properties == nil {
					req.Events[i].Properties = map[string]any{}
				}
				req.Events[i].Properties["_momento_contract_warnings"] = warnings
			}
		}
	}
	var privacy privacyConfig
	_ = json.Unmarshal(site.PrivacyRaw, &privacy)
	privacyBlocked := countPrivacyBlocked(req, privacy.BlockedProperties)
	applyPrivacyBeforeQueue(&req, &clientIP, &userAgent, privacy)
	payload := model.InboxPayload{Request: req, ClientIP: clientIP, Origin: origin, UserAgent: userAgent, ReceivedUnix: time.Now().UnixMilli(), PrivacyBlocked: privacyBlocked}
	body, err := payload.JSON()
	if err != nil {
		return 0, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	receivedDate := time.Now()
	if location, loadErr := time.LoadLocation(site.Timezone); loadErr == nil {
		receivedDate = receivedDate.In(location)
	}
	for _, event := range req.Events {
		warningCount := int64(0)
		if _, ok := event.Properties["_momento_contract_warnings"]; ok {
			warningCount = 1
		}
		if _, err := tx.Exec(ctx, `INSERT INTO data_quality_daily(site_id,event_date,environment,event_name,received,warnings)
			VALUES($1,$2,$3,$4,1,$5) ON CONFLICT(site_id,event_date,environment,event_name) DO UPDATE SET received=data_quality_daily.received+1,warnings=data_quality_daily.warnings+excluded.warnings,updated_at=now()`, site.ID, receivedDate.Format("2006-01-02"), req.Environment, event.Name, warningCount); err != nil {
			return 0, err
		}
	}
	var inboxID int64
	if err = tx.QueryRow(ctx, `INSERT INTO event_inbox(site_id,payload) VALUES($1,$2) RETURNING id`, site.ID, body).Scan(&inboxID); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return inboxID, nil
}

func strictestValidationMode(a, b string) string {
	rank := map[string]int{"allow": 0, "warn": 1, "reject": 2}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

func (s CollectorService) recordQualityRejection(ctx context.Context, site SiteForCollect, environment string, event model.IncomingEvent, warnings []string) {
	date := time.Now()
	if location, err := time.LoadLocation(site.Timezone); err == nil {
		date = date.In(location)
	}
	_, _ = s.DB.Exec(ctx, `INSERT INTO data_quality_daily(site_id,event_date,environment,event_name,received,rejected) VALUES($1,$2,$3,$4,1,1)
		ON CONFLICT(site_id,event_date,environment,event_name) DO UPDATE SET received=data_quality_daily.received+1,rejected=data_quality_daily.rejected+1,updated_at=now()`, site.ID, date.Format("2006-01-02"), environment, event.Name)
	sample, _ := json.Marshal(map[string]any{"warnings": warnings, "contract_version": event.ContractVersion})
	_, _ = s.DB.Exec(ctx, `INSERT INTO data_quality_issues(site_id,environment,event_name,code,severity,message,sample) VALUES($1,$2,$3,'CONTRACT_REJECTED','error',$4,$5)`, site.ID, environment, event.Name, strings.Join(warnings, "; "), sample)
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
	if p.Request.Environment == "" {
		p.Request.Environment = "prd"
	}
	var timezone string
	var cardinalityLimit int
	if err := tx.QueryRow(ctx, `SELECT s.timezone,e.cardinality_limit FROM sites s JOIN site_environments e ON e.site_id=s.id AND e.name=$2 WHERE s.id=$1`, siteID, p.Request.Environment).Scan(&timezone, &cardinalityLimit); err != nil {
		return err
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return fmt.Errorf("invalid site timezone %q: %w", timezone, err)
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
		if event.ContractVersion <= 0 {
			event.ContractVersion = 1
		}
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
		result, err := tx.Exec(ctx, `INSERT INTO raw_events(event_id,site_id,event_name,event_timestamp,visitor_id,session_id,user_id,page_url,page_title,referrer,source,medium,campaign,device_type,browser,os,language,screen,user_agent,client_ip,network_name,properties,user_properties,is_conversion,is_internal,traffic_class,environment,contract_version)
			VALUES($1,$2,$3,$4,$5,$6,nullif($7,''),nullif($8,''),nullif($9,''),nullif($10,''),nullif($11,''),nullif($12,''),nullif($13,''),nullif($14,''),nullif($15,''),nullif($16,''),nullif($17,''),nullif($18,''),nullif($19,''),$20,$21,$22,$23,$24,$25,$26,$27,$28)
			ON CONFLICT(site_id,event_id) DO NOTHING`, eventID, siteID, event.Name, eventTime, p.Request.VisitorID, p.Request.SessionID, userID, pageURL, ctxValue.Page.Title, ctxValue.Page.Referrer, ctxValue.Traffic.Source, ctxValue.Traffic.Medium, ctxValue.Traffic.Campaign, ctxValue.Device.Type, ctxValue.Device.Browser, ctxValue.Device.OS, ctxValue.Device.Language, ctxValue.Device.Screen, userAgent, nullableIP(ip), networkName, propsJSON, userJSON, conversion, internal, trafficClass, p.Request.Environment, event.ContractVersion)
		if err != nil {
			return err
		}
		if result.RowsAffected() > 0 {
			if err := recordAcceptedQuality(ctx, tx, siteID, p, event, eventTime, location, userID, networkName); err != nil {
				return err
			}
			p.PrivacyBlocked = 0
			if err := recordCardinality(ctx, tx, siteID, p.Request.Environment, eventTime, location, event, pageURL, networkName, cardinalityLimit); err != nil {
				return err
			}
			if userID != "" {
				if err := updateVisitorIdentity(ctx, tx, siteID, p.Request.VisitorID, userID, eventTime); err != nil {
					return err
				}
			}
			canonicalUserID, err := updateVisitor(ctx, tx, siteID, p.Request.VisitorID, userID, userJSON, eventTime, conversion)
			if err != nil {
				return err
			}
			if canonicalUserID != "" {
				if _, err := tx.Exec(ctx, `UPDATE visitor_identities SET last_seen=greatest(last_seen,$3),updated_at=now() WHERE site_id=$1 AND visitor_id=$2`, siteID, p.Request.VisitorID, eventTime); err != nil {
					return err
				}
				if err := updateIdentifiedUser(ctx, tx, siteID, p.Request.VisitorID, canonicalUserID, userJSON, eventTime); err != nil {
					return err
				}
			}
			if err := updateSession(ctx, tx, siteID, p.Request, event, eventTime, canonicalUserID, pageURL, ctxValue, conversion); err != nil {
				return err
			}
			if err := updateVisitorSession(ctx, tx, siteID, p.Request.VisitorID, p.Request.SessionID, canonicalUserID, eventTime); err != nil {
				return err
			}
			if err := updateDailyAggregates(ctx, tx, siteID, p.Request.Environment, p.Request.VisitorID, p.Request.SessionID, canonicalUserID, userJSON, event, eventTime, location, conversion); err != nil {
				return err
			}
		} else {
			local := eventTime.In(location)
			_, _ = tx.Exec(ctx, `INSERT INTO data_quality_daily(site_id,event_date,environment,event_name,duplicates) VALUES($1,$2,$3,$4,1)
				ON CONFLICT(site_id,event_date,environment,event_name) DO UPDATE SET duplicates=data_quality_daily.duplicates+1,updated_at=now()`, siteID, local.Format("2006-01-02"), p.Request.Environment, event.Name)
		}
	}
	return nil
}

func updateVisitorIdentity(ctx context.Context, tx pgx.Tx, siteID uuid.UUID, visitorID, userID string, eventTime time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO visitor_identities(site_id,visitor_id,user_id,first_seen,linked_at,last_seen)
		VALUES($1,$2,$3,coalesce((SELECT first_seen FROM visitors WHERE site_id=$1 AND visitor_id=$2),$4),$4,$4)
		ON CONFLICT(site_id,visitor_id) DO UPDATE SET
		user_id=CASE WHEN excluded.last_seen>=visitor_identities.last_seen THEN excluded.user_id ELSE visitor_identities.user_id END,
		first_seen=least(visitor_identities.first_seen,excluded.first_seen),
		linked_at=CASE
			WHEN excluded.last_seen<visitor_identities.last_seen THEN visitor_identities.linked_at
			WHEN visitor_identities.user_id=excluded.user_id THEN least(visitor_identities.linked_at,excluded.linked_at)
			ELSE excluded.linked_at
		END,
		last_seen=greatest(visitor_identities.last_seen,excluded.last_seen),
		confidence=1.000,source='identify',updated_at=now()`, siteID, visitorID, userID, eventTime)
	if err != nil {
		return err
	}
	// Identity is deterministic. Once a visitor is linked, all summaries expose
	// the canonical user even for events that happened before identify().
	if _, err = tx.Exec(ctx, `UPDATE visitors v SET user_id=i.user_id,updated_at=now() FROM visitor_identities i WHERE v.site_id=$1 AND v.visitor_id=$2 AND i.site_id=v.site_id AND i.visitor_id=v.visitor_id`, siteID, visitorID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE visitor_sessions v SET user_id=i.user_id,updated_at=now() FROM visitor_identities i WHERE v.site_id=$1 AND v.visitor_id=$2 AND i.site_id=v.site_id AND i.visitor_id=v.visitor_id`, siteID, visitorID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE daily_site_visitors v SET user_id=i.user_id,updated_at=now() FROM visitor_identities i WHERE v.site_id=$1 AND v.visitor_id=$2 AND i.site_id=v.site_id AND i.visitor_id=v.visitor_id`, siteID, visitorID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE daily_site_sessions v SET user_id=i.user_id,updated_at=now() FROM visitor_identities i WHERE v.site_id=$1 AND v.visitor_id=$2 AND i.site_id=v.site_id AND i.visitor_id=v.visitor_id`, siteID, visitorID); err != nil {
		return err
	}
	// A deterministic reassignment may leave the previous user without any
	// linked visitors. Remove that unreachable profile in the same transaction.
	_, err = tx.Exec(ctx, `DELETE FROM identified_users u WHERE u.site_id=$1 AND NOT EXISTS(SELECT 1 FROM visitor_identities i WHERE i.site_id=u.site_id AND i.user_id=u.user_id)`, siteID)
	return err
}

func updateVisitor(ctx context.Context, tx pgx.Tx, siteID uuid.UUID, visitorID, userID string, userProperties []byte, eventTime time.Time, conversion bool) (string, error) {
	var canonicalUserID string
	err := tx.QueryRow(ctx, `INSERT INTO visitors(site_id,visitor_id,user_id,first_seen,last_seen,event_count,conversion_count,user_properties)
		VALUES($1,$2,coalesce((SELECT user_id FROM visitor_identities WHERE site_id=$1 AND visitor_id=$2),nullif($3,'')),$4,$4,1,$5,$6)
		ON CONFLICT(site_id,visitor_id) DO UPDATE SET
		user_id=coalesce((SELECT user_id FROM visitor_identities WHERE site_id=$1 AND visitor_id=$2),excluded.user_id,visitors.user_id),
		first_seen=least(visitors.first_seen,excluded.first_seen),
		last_seen=greatest(visitors.last_seen,excluded.last_seen),
		event_count=visitors.event_count+1,
		conversion_count=visitors.conversion_count+excluded.conversion_count,
		user_properties=CASE WHEN excluded.user_properties<>'{}'::jsonb AND excluded.last_seen>=visitors.last_seen THEN excluded.user_properties ELSE visitors.user_properties END,
		updated_at=now()
		RETURNING coalesce(user_id,'')`, siteID, visitorID, userID, eventTime, boolInt(conversion), userProperties).Scan(&canonicalUserID)
	return canonicalUserID, err
}

func updateIdentifiedUser(ctx context.Context, tx pgx.Tx, siteID uuid.UUID, visitorID, userID string, userProperties []byte, eventTime time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO identified_users(site_id,user_id,first_seen,last_seen,user_properties)
		VALUES($1,$2,coalesce((SELECT first_seen FROM visitors WHERE site_id=$1 AND visitor_id=$3),$4),$4,$5)
		ON CONFLICT(site_id,user_id) DO UPDATE SET
		first_seen=least(identified_users.first_seen,excluded.first_seen),
		last_seen=greatest(identified_users.last_seen,excluded.last_seen),
		user_properties=CASE WHEN excluded.user_properties<>'{}'::jsonb AND excluded.last_seen>=identified_users.last_seen THEN excluded.user_properties ELSE identified_users.user_properties END,
		updated_at=now()`, siteID, userID, visitorID, eventTime, userProperties)
	return err
}

func updateVisitorSession(ctx context.Context, tx pgx.Tx, siteID uuid.UUID, visitorID, sessionID, userID string, eventTime time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO visitor_sessions(site_id,visitor_id,session_id,user_id,first_seen,last_seen)
		VALUES($1,$2,$3,nullif($4,''),$5,$5)
		ON CONFLICT(site_id,visitor_id,session_id) DO UPDATE SET
		user_id=coalesce(excluded.user_id,visitor_sessions.user_id),
		first_seen=least(visitor_sessions.first_seen,excluded.first_seen),
		last_seen=greatest(visitor_sessions.last_seen,excluded.last_seen),updated_at=now()`, siteID, visitorID, sessionID, userID, eventTime)
	return err
}

func updateDailyAggregates(ctx context.Context, tx pgx.Tx, siteID uuid.UUID, environment, visitorID, sessionID, userID string, userProperties []byte, event model.IncomingEvent, eventTime time.Time, location *time.Location, conversion bool) error {
	local := eventTime.In(location)
	eventDate := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	if _, err := tx.Exec(ctx, `INSERT INTO daily_site_metrics(site_id,event_date,environment,events,page_views,conversions,revenue)
		VALUES($1,$2,$3,1,$4,$5,$6)
		ON CONFLICT(site_id,event_date,environment) DO UPDATE SET
		events=daily_site_metrics.events+1,
		page_views=daily_site_metrics.page_views+excluded.page_views,
		conversions=daily_site_metrics.conversions+excluded.conversions,
		revenue=daily_site_metrics.revenue+excluded.revenue,updated_at=now()`, siteID, eventDate, environment, boolInt(event.Name == "page_view"), boolInt(conversion), eventRevenue(event)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO daily_site_visitors(site_id,event_date,environment,visitor_id,user_id,first_seen,last_seen,event_count,conversion_count,user_properties)
		VALUES($1,$2,$3,$4,nullif($5,''),$6,$6,1,$7,$8)
		ON CONFLICT(site_id,event_date,environment,visitor_id) DO UPDATE SET
		user_id=coalesce(excluded.user_id,daily_site_visitors.user_id),
		first_seen=least(daily_site_visitors.first_seen,excluded.first_seen),
		last_seen=greatest(daily_site_visitors.last_seen,excluded.last_seen),
		event_count=daily_site_visitors.event_count+1,
		conversion_count=daily_site_visitors.conversion_count+excluded.conversion_count,
		user_properties=CASE WHEN excluded.user_properties<>'{}'::jsonb AND excluded.last_seen>=daily_site_visitors.last_seen THEN excluded.user_properties ELSE daily_site_visitors.user_properties END,
		updated_at=now()`, siteID, eventDate, environment, visitorID, userID, eventTime, boolInt(conversion), userProperties); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO daily_site_sessions(site_id,event_date,environment,session_id,visitor_id,user_id,first_seen,last_seen)
		VALUES($1,$2,$3,$4,$5,nullif($6,''),$7,$7)
		ON CONFLICT(site_id,event_date,environment,session_id) DO UPDATE SET
		visitor_id=excluded.visitor_id,user_id=coalesce(excluded.user_id,daily_site_sessions.user_id),
		first_seen=least(daily_site_sessions.first_seen,excluded.first_seen),
		last_seen=greatest(daily_site_sessions.last_seen,excluded.last_seen),updated_at=now()`, siteID, eventDate, environment, sessionID, visitorID, userID, eventTime)
	return err
}

func eventRevenue(event model.IncomingEvent) float64 {
	if event.Name != "purchase" {
		return 0
	}
	for _, key := range []string{"value", "revenue"} {
		switch value := event.Properties[key].(type) {
		case float64:
			return value
		case float32:
			return float64(value)
		case int:
			return float64(value)
		case int64:
			return float64(value)
		case json.Number:
			if parsed, err := value.Float64(); err == nil {
				return parsed
			}
		case string:
			if parsed, err := strconv.ParseFloat(value, 64); err == nil {
				return parsed
			}
		}
	}
	return 0
}

func updateSession(ctx context.Context, tx pgx.Tx, siteID uuid.UUID, request model.CollectRequest, event model.IncomingEvent, eventTime time.Time, userID, pageURL string, eventContext model.EventContext, conversion bool) error {
	var page any
	if event.Name == "page_view" && pageURL != "" {
		page = pageURL
	}
	activeMS := activeEngagementMilliseconds(event)
	heartbeat := boolInt(event.Name == "user_engagement")
	interaction := boolInt(isInteractionEvent(event.Name))
	_, err := tx.Exec(ctx, `INSERT INTO sessions(site_id,session_id,visitor_id,user_id,started_at,last_event_at,event_count,page_views,conversion_count,engaged,landing_page,exit_page,source,medium,campaign,device_type,active_engagement_ms,heartbeat_count,interaction_count,environment)
		VALUES($1,$2,$3,nullif($4,''),$5,$5,1,$6::bigint,$7::bigint,($7::bigint>0 OR $6::bigint>=2 OR $13::bigint >= (SELECT engagement_threshold_seconds::bigint*1000 FROM sites WHERE id=$1)),$8,$8,nullif($9,''),nullif($10,''),nullif($11,''),nullif($12,''),$13::bigint,$14::bigint,$15::bigint,$16)
		ON CONFLICT(site_id,environment,session_id) DO UPDATE SET
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
		environment=CASE WHEN excluded.last_event_at>=sessions.last_event_at THEN excluded.environment ELSE sessions.environment END,
		updated_at=now()`, siteID, request.SessionID, request.VisitorID, userID, eventTime, boolInt(event.Name == "page_view"), boolInt(conversion), page, eventContext.Traffic.Source, eventContext.Traffic.Medium, eventContext.Traffic.Campaign, eventContext.Device.Type, activeMS, heartbeat, interaction, request.Environment)
	return err
}

func recordAcceptedQuality(ctx context.Context, tx pgx.Tx, siteID uuid.UUID, p model.InboxPayload, event model.IncomingEvent, eventTime time.Time, location *time.Location, userID, networkName string) error {
	local := eventTime.In(location)
	late := int64(0)
	if p.ReceivedUnix > 0 && time.UnixMilli(p.ReceivedUnix).Sub(eventTime).Abs() > time.Hour {
		late = 1
	}
	missingUser, missingFeature, unknownNetwork := int64(0), int64(0), int64(0)
	if userID == "" {
		missingUser = 1
	}
	if strings.TrimSpace(fmt.Sprint(event.Properties["feature"])) == "" || event.Properties["feature"] == nil {
		missingFeature = 1
	}
	if networkName == "External / Unclassified" {
		unknownNetwork = 1
	}
	_, err := tx.Exec(ctx, `INSERT INTO data_quality_daily(site_id,event_date,environment,event_name,accepted,late_events,missing_user_id,missing_feature,unknown_network,pii_blocked)
		VALUES($1,$2,$3,$4,1,$5,$6,$7,$8,$9) ON CONFLICT(site_id,event_date,environment,event_name) DO UPDATE SET accepted=data_quality_daily.accepted+1,late_events=data_quality_daily.late_events+excluded.late_events,missing_user_id=data_quality_daily.missing_user_id+excluded.missing_user_id,missing_feature=data_quality_daily.missing_feature+excluded.missing_feature,unknown_network=data_quality_daily.unknown_network+excluded.unknown_network,pii_blocked=data_quality_daily.pii_blocked+excluded.pii_blocked,updated_at=now()`, siteID, local.Format("2006-01-02"), p.Request.Environment, event.Name, late, missingUser, missingFeature, unknownNetwork, p.PrivacyBlocked)
	return err
}

func recordCardinality(ctx context.Context, tx pgx.Tx, siteID uuid.UUID, environment string, eventTime time.Time, location *time.Location, event model.IncomingEvent, pageURL, networkName string, limit int) error {
	values := map[string]string{
		"event_name": event.Name, "page": pageURL, "network": networkName,
		"feature": stringProperty(event.Properties, "feature"), "department": stringProperty(event.Properties, "department"),
		"organization": stringProperty(event.Properties, "organization"), "release_version": stringProperty(event.Properties, "release_version"),
		"model": stringProperty(event.Properties, "model"), "agent": stringProperty(event.Properties, "agent"), "tool": stringProperty(event.Properties, "tool"),
	}
	date := eventTime.In(location).Format("2006-01-02")
	for dimension, value := range values {
		if value == "" {
			continue
		}
		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(value)))
		result, err := tx.Exec(ctx, `INSERT INTO data_quality_dimension_values(site_id,event_date,environment,dimension,value_hash) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, siteID, date, environment, dimension, hash)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			continue
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM data_quality_dimension_values WHERE site_id=$1 AND event_date=$2 AND environment=$3 AND dimension=$4`, siteID, date, environment, dimension).Scan(&count); err != nil {
			return err
		}
		if count == limit+1 {
			_, _ = tx.Exec(ctx, `UPDATE data_quality_daily SET cardinality_violations=cardinality_violations+1,updated_at=now() WHERE site_id=$1 AND event_date=$2 AND environment=$3 AND event_name=$4`, siteID, date, environment, event.Name)
			_, _ = tx.Exec(ctx, `INSERT INTO data_quality_issues(site_id,environment,event_name,code,severity,message,sample) VALUES($1,$2,$3,'CARDINALITY_LIMIT','warning',$4,jsonb_build_object('dimension',$5,'limit',$6))`, siteID, environment, event.Name, fmt.Sprintf("%s exceeded the daily cardinality limit", dimension), dimension, limit)
		}
	}
	return nil
}

func stringProperty(properties map[string]any, key string) string {
	value, ok := properties[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
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

func countPrivacyBlocked(req model.CollectRequest, blocked []string) int {
	blockedSet := make(map[string]bool, len(blocked))
	for _, key := range blocked {
		blockedSet[strings.ToLower(strings.TrimSpace(key))] = true
	}
	count := countBlockedProperties(req.UserProperties, blockedSet)
	for _, event := range req.Events {
		count += countBlockedProperties(event.Properties, blockedSet)
		for _, item := range event.Items {
			count += countBlockedProperties(item, blockedSet)
		}
	}
	return count
}

func countBlockedProperties(properties map[string]any, blocked map[string]bool) int {
	count := 0
	for key, value := range properties {
		if blocked[strings.ToLower(key)] {
			count++
			continue
		}
		switch nested := value.(type) {
		case map[string]any:
			count += countBlockedProperties(nested, blocked)
		case []any:
			for _, item := range nested {
				if object, ok := item.(map[string]any); ok {
					count += countBlockedProperties(object, blocked)
				}
			}
		}
	}
	return count
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
	if _, err := w.DB.Exec(ctx, `DELETE FROM data_quality_dimension_values d WHERE d.event_date < current_date-make_interval(days=>$1)`, debugDays); err != nil {
		return err
	}
	if _, err := w.DB.Exec(ctx, `DELETE FROM data_quality_issues d WHERE d.occurred_at < now()-make_interval(days=>$1)`, debugDays); err != nil {
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
