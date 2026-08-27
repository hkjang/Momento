package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hkjang/Momento/internal/insight"
	"github.com/hkjang/Momento/internal/secret"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// reportWindow returns the period a delivery covers, in the site's calendar and
// ending at local midnight, so it matches the window the console reads.
func (a Automation) reportWindow(ctx context.Context, siteID uuid.UUID, days int) (time.Time, time.Time, error) {
	var timezone string
	if err := a.DB.QueryRow(ctx, `SELECT timezone FROM sites WHERE id=$1`, siteID).Scan(&timezone); err != nil {
		return time.Time{}, time.Time{}, err
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid site timezone %q: %w", timezone, err)
	}
	now := time.Now().In(location)
	to := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location).AddDate(0, 0, 1)
	return to.AddDate(0, 0, -days).UTC(), to.UTC(), nil
}

// ErrSkipDelivery lets a report decide that there is nothing worth sending. An
// alert channel that fires every hour with "nothing wrong" stops being read.
var ErrSkipDelivery = errors.New("nothing to deliver")

type Automation struct {
	DB      *pgxpool.Pool
	Logger  *slog.Logger
	Secrets *secret.Cipher
}

type automationConfig struct {
	Enabled                bool     `json:"enabled"`
	AllowedWebhookHosts    []string `json:"allowed_webhook_hosts"`
	DeliveryTimeoutSeconds int      `json:"delivery_timeout_seconds"`
	MaxEntityIDs           int      `json:"max_entity_ids"`
}

type scheduledDelivery struct {
	ReportID       uuid.UUID
	SiteID         uuid.UUID
	SiteKey        string
	ChannelID      uuid.UUID
	ChannelType    string
	EndpointURL    string
	Headers        []byte
	HeadersSecret  *string
	Name           string
	ReportKind     string
	Definition     []byte
	IntervalMinute int
}

func (a Automation) Run(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for index := 0; index < 20; index++ {
				ran, err := a.runNext(ctx)
				if err != nil && ctx.Err() == nil && a.Logger != nil {
					a.Logger.Error("scheduled delivery failed", "error", err)
				}
				if !ran {
					break
				}
			}
		}
	}
}

func (a Automation) config(ctx context.Context) (automationConfig, error) {
	var raw []byte
	var config automationConfig
	if err := a.DB.QueryRow(ctx, `SELECT value FROM settings WHERE key='automation'`).Scan(&raw); err != nil {
		return config, err
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return config, err
	}
	if config.DeliveryTimeoutSeconds < 1 || config.DeliveryTimeoutSeconds > 60 {
		config.DeliveryTimeoutSeconds = 10
	}
	if config.MaxEntityIDs < 0 || config.MaxEntityIDs > 1000 {
		config.MaxEntityIDs = 0
	}
	return config, nil
}

func (a Automation) runNext(ctx context.Context) (bool, error) {
	config, err := a.config(ctx)
	if err != nil || !config.Enabled {
		return false, err
	}
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var delivery scheduledDelivery
	err = tx.QueryRow(ctx, `SELECT r.id,r.site_id,s.site_key,c.id,c.channel_type,c.endpoint_url,c.headers,c.headers_secret,r.name,r.report_kind,r.definition,r.interval_minutes
		FROM scheduled_reports r JOIN delivery_channels c ON c.id=r.channel_id AND c.site_id=r.site_id AND c.active JOIN sites s ON s.id=r.site_id AND s.active
		WHERE r.enabled AND r.next_run_at<=now() ORDER BY r.next_run_at LIMIT 1 FOR UPDATE OF r SKIP LOCKED`).Scan(&delivery.ReportID, &delivery.SiteID, &delivery.SiteKey, &delivery.ChannelID, &delivery.ChannelType, &delivery.EndpointURL, &delivery.Headers, &delivery.HeadersSecret, &delivery.Name, &delivery.ReportKind, &delivery.Definition, &delivery.IntervalMinute)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE scheduled_reports SET next_run_at=now()+make_interval(mins=>$2),last_run_at=now(),last_status='running',last_error=NULL,updated_at=now() WHERE id=$1`, delivery.ReportID, delivery.IntervalMinute); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	err = a.execute(ctx, delivery, config)
	return true, err
}

func (a Automation) RunByID(ctx context.Context, reportID uuid.UUID) error {
	config, err := a.config(ctx)
	if err != nil {
		return err
	}
	if !config.Enabled {
		return fmt.Errorf("automation is disabled in administrator settings")
	}
	var delivery scheduledDelivery
	err = a.DB.QueryRow(ctx, `SELECT r.id,r.site_id,s.site_key,c.id,c.channel_type,c.endpoint_url,c.headers,c.headers_secret,r.name,r.report_kind,r.definition,r.interval_minutes
		FROM scheduled_reports r JOIN delivery_channels c ON c.id=r.channel_id AND c.site_id=r.site_id AND c.active JOIN sites s ON s.id=r.site_id AND s.active WHERE r.id=$1`, reportID).Scan(&delivery.ReportID, &delivery.SiteID, &delivery.SiteKey, &delivery.ChannelID, &delivery.ChannelType, &delivery.EndpointURL, &delivery.Headers, &delivery.HeadersSecret, &delivery.Name, &delivery.ReportKind, &delivery.Definition, &delivery.IntervalMinute)
	if err != nil {
		return err
	}
	_, _ = a.DB.Exec(ctx, `UPDATE scheduled_reports SET last_run_at=now(),last_status='running',last_error=NULL,updated_at=now() WHERE id=$1`, reportID)
	return a.execute(ctx, delivery, config)
}

func (a Automation) execute(ctx context.Context, delivery scheduledDelivery, config automationConfig) error {
	started := time.Now()
	payload, err := a.buildPayload(ctx, delivery, config)
	if err == nil {
		err = validateDeliveryEndpoint(delivery.EndpointURL, config.AllowedWebhookHosts)
	}
	var status int
	if err == nil {
		var headers map[string]string
		headers, err = a.deliveryHeaders(delivery)
		if err == nil {
			status, err = postDelivery(ctx, delivery, payload, config.DeliveryTimeoutSeconds, headers)
		}
	}
	state := "success"
	errorText := ""
	switch {
	case errors.Is(err, ErrSkipDelivery):
		// Nothing to report is a normal outcome, not a failure.
		state, errorText, err = "skipped", "", nil
	case err != nil:
		state, errorText = "failed", truncateAutomationError(err.Error())
	}
	_, _ = a.DB.Exec(ctx, `INSERT INTO delivery_runs(site_id,report_id,channel_id,status,response_status,error,started_at,finished_at) VALUES($1,$2,$3,$4,$5,nullif($6,''),$7,now())`, delivery.SiteID, delivery.ReportID, delivery.ChannelID, state, nullableStatus(status), errorText, started)
	_, _ = a.DB.Exec(ctx, `UPDATE scheduled_reports SET last_status=$2,last_error=nullif($3,''),updated_at=now() WHERE id=$1`, delivery.ReportID, state, errorText)
	return err
}

func (a Automation) buildPayload(ctx context.Context, delivery scheduledDelivery, config automationConfig) (map[string]any, error) {
	definition := map[string]any{}
	_ = json.Unmarshal(delivery.Definition, &definition)
	environment, _ := definition["environment"].(string)
	if environment == "" {
		environment = "prd"
	}
	days := 7
	if value, ok := definition["days"].(float64); ok && value >= 1 && value <= 365 {
		days = int(value)
	}
	// The same period the screen shows. Reports are read in the site's calendar,
	// so a seven day digest has to mean the last seven local days and end at local
	// midnight; measuring from the moment the schedule happened to fire gave a
	// number that never matched the screen it was named after, and moved with the
	// send time.
	from, to, err := a.reportWindow(ctx, delivery.SiteID, days)
	if err != nil {
		return nil, err
	}
	data := map[string]any{}
	switch delivery.ReportKind {
	case "overview", "insights":
		var users, events, conversions, errors int64
		var revenue float64
		err := a.DB.QueryRow(ctx, `SELECT count(DISTINCT entity_id),count(*),count(*) FILTER(WHERE is_conversion),count(*) FILTER(WHERE event_name=ANY($5)),`+insight.RevenueAmountSQL("")+`::double precision FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3`, delivery.SiteID, from, to, environment, []string{"error", "resource_error"}).Scan(&users, &events, &conversions, &errors, &revenue)
		if err != nil {
			return nil, err
		}
		// Sessions come from the sessions table and are counted by when they
		// started, which is the definition every screen uses. A digest that omits
		// sessions or counts them differently is a digest of a different report.
		var sessions, engaged int64
		if err := a.DB.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE engaged) FROM sessions
			WHERE site_id=$1 AND environment=$2 AND started_at >= $3 AND started_at < $4`,
			delivery.SiteID, environment, from, to).Scan(&sessions, &engaged); err != nil {
			return nil, err
		}
		data = map[string]any{"users": users, "sessions": sessions, "engaged_sessions": engaged,
			"events": events, "conversions": conversions, "errors": errors, "revenue": revenue,
			"from": from, "to": to}
	case "adoption":
		// The adoption screen's own numbers. This used to run a separate query that
		// returned feature events and users, which is the feature intelligence
		// report — a digest named after one screen carrying another's content, with
		// no adoption rate in it.
		rows, err := insight.New(a.DB).Adoption(ctx, delivery.SiteID, environment, from, to, 50)
		if err != nil {
			return nil, err
		}
		data = map[string]any{"features": rows, "from": from, "to": to}
	case "experience":
		var errors, affected int64
		err := a.DB.QueryRow(ctx, `SELECT count(*),count(DISTINCT entity_id) FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3 AND event_name=ANY($5)`, delivery.SiteID, from, to, environment, []string{"error", "resource_error"}).Scan(&errors, &affected)
		if err != nil {
			return nil, err
		}
		data = map[string]any{"errors": errors, "affected_users": affected}
	case "ai":
		var calls, users, inputTokens, outputTokens int64
		err := a.DB.QueryRow(ctx, `SELECT count(*),count(DISTINCT entity_id),coalesce(sum(CASE WHEN coalesce(properties->>'input_tokens','')~'^[0-9]+$' THEN (properties->>'input_tokens')::bigint ELSE 0 END),0),coalesce(sum(CASE WHEN coalesce(properties->>'output_tokens','')~'^[0-9]+$' THEN (properties->>'output_tokens')::bigint ELSE 0 END),0) FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3 AND event_name=ANY($5)`, delivery.SiteID, from, to, environment, []string{"ai_prompt", "ai_response", "ai_tool_call", "ai_agent_run", "ai_mcp_call", "ai_model_call"}).Scan(&calls, &users, &inputTokens, &outputTokens)
		if err != nil {
			return nil, err
		}
		data = map[string]any{"calls": calls, "users": users, "input_tokens": inputTokens, "output_tokens": outputTokens}
	case "anomaly":
		location := time.UTC
		var timezone string
		if a.DB.QueryRow(ctx, `SELECT timezone FROM sites WHERE id=$1`, delivery.SiteID).Scan(&timezone) == nil {
			if loaded, loadErr := time.LoadLocation(timezone); loadErr == nil {
				location = loaded
			}
		}
		reporter := insight.New(a.DB)
		report, err := reporter.DetectSiteAnomalies(ctx, delivery.SiteID, environment, location)
		if err != nil {
			return nil, err
		}
		notifyOn := insight.NotifiableStates()
		if raw, ok := definition["notify_on"].([]any); ok && len(raw) > 0 {
			notifyOn = notifyOn[:0]
			for _, item := range raw {
				if state, ok := item.(string); ok {
					notifyOn = append(notifyOn, state)
				}
			}
		}
		// Alert state turns detections into transitions, so an open anomaly is
		// announced once instead of on every schedule tick.
		announce, err := reporter.ApplyAnomalyState(ctx, delivery.SiteID, environment, report, notifyOn)
		if err != nil {
			return nil, err
		}
		alwaysSend, _ := definition["always_send"].(bool)
		if len(announce) == 0 && !alwaysSend {
			return nil, ErrSkipDelivery
		}
		data = map[string]any{"evaluated_date": report.EvaluatedDate, "timezone": report.Timezone, "baseline_weeks": report.BaselineWeeks,
			"announced": announce, "notify_on": notifyOn, "detected": report.Detected, "checked": report.Checked, "note": report.Note}
	case "visitor_insight":
		// Deliver the same visitor insight report the console shows, so a mailed or
		// Confluence-published digest needs no manual assembly.
		report, err := insight.New(a.DB).Build(ctx, delivery.SiteID, environment, from, to, from.Add(-to.Sub(from)), from)
		if err != nil {
			return nil, err
		}
		data = report
	case "segment":
		eventName, _ := definition["event_name"].(string)
		feature, _ := definition["feature"].(string)
		department, _ := definition["department"].(string)
		var count int64
		err := a.DB.QueryRow(ctx, `SELECT count(DISTINCT entity_id) FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3 AND ($5='' OR event_name=$5) AND ($6='' OR properties->>'feature'=$6) AND ($7='' OR canonical_user_properties->>'department'=$7)`, delivery.SiteID, from, to, environment, eventName, feature, department).Scan(&count)
		if err != nil {
			return nil, err
		}
		data = map[string]any{"matched_entities": count, "event_name": eventName, "feature": feature, "department": department}
		if config.MaxEntityIDs > 0 {
			rows, queryErr := a.DB.Query(ctx, `SELECT DISTINCT entity_id FROM analytics_events WHERE site_id=$1 AND environment=$4 AND event_timestamp >= $2 AND event_timestamp < $3 AND ($5='' OR event_name=$5) AND ($6='' OR properties->>'feature'=$6) AND ($7='' OR canonical_user_properties->>'department'=$7) LIMIT $8`, delivery.SiteID, from, to, environment, eventName, feature, department, config.MaxEntityIDs)
			if queryErr != nil {
				return nil, queryErr
			}
			defer rows.Close()
			entities := []string{}
			for rows.Next() {
				var entity string
				if rows.Scan(&entity) == nil {
					entities = append(entities, entity)
				}
			}
			data["entity_ids"] = entities
		}
	default:
		return nil, fmt.Errorf("unsupported report kind %q", delivery.ReportKind)
	}
	payload := map[string]any{"source": "Momento", "report_id": delivery.ReportID, "site_id": delivery.SiteKey, "name": delivery.Name, "kind": delivery.ReportKind, "environment": environment, "from": from, "to": to, "generated_at": time.Now().UTC(), "data": data}
	if delivery.ChannelType == "confluence" {
		raw, _ := json.MarshalIndent(payload, "", "  ")
		title, _ := definition["page_title"].(string)
		spaceKey, _ := definition["space_key"].(string)
		if title == "" {
			title = "Momento - " + delivery.Name + " - " + time.Now().Format("2006-01-02")
		}
		payload = map[string]any{"type": "page", "title": title, "space": map[string]any{"key": spaceKey}, "body": map[string]any{"storage": map[string]any{"value": "<h2>Momento Analytics</h2><pre>" + html.EscapeString(string(raw)) + "</pre>", "representation": "storage"}}}
	}
	return payload, nil
}

func validateDeliveryEndpoint(raw string, allowedHosts []string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return fmt.Errorf("delivery endpoint must be an http(s) URL without embedded credentials")
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	for _, allowed := range allowedHosts {
		allowed = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(allowed, ".")))
		if allowed == host || (strings.HasPrefix(allowed, "*.") && strings.HasSuffix(host, allowed[1:])) {
			return nil
		}
	}
	return fmt.Errorf("delivery host %q is not in automation.allowed_webhook_hosts", host)
}

// deliveryHeaders opens the channel credentials. They are sealed with the
// encryption key so a restart keeps them usable without re-entering them.
func (a Automation) deliveryHeaders(delivery scheduledDelivery) (map[string]string, error) {
	headers := map[string]string{}
	if delivery.HeadersSecret != nil && *delivery.HeadersSecret != "" {
		plain, err := a.Secrets.Decrypt(*delivery.HeadersSecret)
		if err != nil {
			return nil, fmt.Errorf("channel headers cannot be decrypted: %w", err)
		}
		if err := json.Unmarshal([]byte(plain), &headers); err != nil {
			return nil, fmt.Errorf("channel headers are malformed: %w", err)
		}
		return headers, nil
	}
	_ = json.Unmarshal(delivery.Headers, &headers)
	return headers, nil
}

func postDelivery(ctx context.Context, delivery scheduledDelivery, payload map[string]any, timeoutSeconds int, headers map[string]string) (int, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, delivery.EndpointURL, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "Momento-Automation/1")
	for key, value := range headers {
		if strings.EqualFold(key, "Host") || strings.ContainsAny(key, "\r\n") || strings.ContainsAny(value, "\r\n") {
			continue
		}
		request.Header.Set(key, value)
	}
	client := &http.Client{Timeout: time.Duration(timeoutSeconds) * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response.StatusCode, fmt.Errorf("delivery returned HTTP %d", response.StatusCode)
	}
	return response.StatusCode, nil
}

func nullableStatus(status int) any {
	if status == 0 {
		return nil
	}
	return status
}

func truncateAutomationError(value string) string {
	if len(value) > 1000 {
		return value[:1000]
	}
	return value
}
