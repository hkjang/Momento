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
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hkjang/Momento/internal/insight"
	"github.com/hkjang/Momento/internal/secret"
	"github.com/hkjang/Momento/internal/segment"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// reportWindow returns the period a delivery covers, in the site's calendar and
// ending at local midnight, so it matches the window the console reads.
func (a Automation) reportWindow(ctx context.Context, siteID uuid.UUID, days int) (time.Time, time.Time, error) {
	_, location, err := a.siteLocation(ctx, siteID)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	now := time.Now().In(location)
	to := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location).AddDate(0, 0, 1)
	return to.AddDate(0, 0, -days).UTC(), to.UTC(), nil
}

// loadSegment reads a saved segment's definition for an unattended delivery.
// There is no principal here: the schedule was created by somebody who could see
// the segment, and the site scope is what keeps it from reaching another site's.
func (a Automation) loadSegment(ctx context.Context, siteID uuid.UUID, id string) (segment.Node, string, error) {
	segmentID, err := uuid.Parse(id)
	if err != nil {
		return segment.Node{}, "", fmt.Errorf("invalid segment id %q", id)
	}
	var raw []byte
	var name string
	if err := a.DB.QueryRow(ctx, `SELECT definition,name FROM segments WHERE id=$1 AND site_id=$2`, segmentID, siteID).Scan(&raw, &name); err != nil {
		return segment.Node{}, "", fmt.Errorf("segment %s not found on this site", id)
	}
	var node segment.Node
	if err := json.Unmarshal(raw, &node); err != nil {
		return segment.Node{}, "", err
	}
	return node, name, nil
}

// siteLocation is the site's calendar. Every period a delivery reports on is
// read in it, so a week means the site's week.
func (a Automation) siteLocation(ctx context.Context, siteID uuid.UUID) (string, *time.Location, error) {
	var timezone string
	if err := a.DB.QueryRow(ctx, `SELECT timezone FROM sites WHERE id=$1`, siteID).Scan(&timezone); err != nil {
		return "", nil, err
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return timezone, nil, fmt.Errorf("invalid site timezone %q: %w", timezone, err)
	}
	return timezone, location, nil
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
		// Both sent the period's absolute totals and nothing else.
		//
		// A weekly summary that says "12,043 users" says nothing: the reader
		// cannot tell whether that is the best week of the year or half of last
		// week's, and the screen it is named after puts the previous period
		// beside every figure. And "인사이트 요약" delivered no insights at all —
		// the same five numbers under a name that promises the findings, which is
		// the shape of wrong answer nobody notices, because there is nothing in
		// it to be suspicious of.
		_, location, tzErr := a.siteLocation(ctx, delivery.SiteID)
		if tzErr != nil {
			return nil, tzErr
		}
		previousFrom, previousTo := insight.PreviousRange(from, to, location)
		reporter := insight.New(a.DB)
		current, err := reporter.Metrics(ctx, delivery.SiteID, environment, from, to)
		if err != nil {
			return nil, err
		}
		previous, err := reporter.Metrics(ctx, delivery.SiteID, environment, previousFrom, previousTo)
		if err != nil {
			return nil, err
		}
		data = map[string]any{
			"current": insight.MetricMap(current), "previous": insight.MetricMap(previous),
			"change_percent": insight.MetricChange(current, previous),
			"previous_from":  previousFrom, "previous_to": previousTo,
			"from": from, "to": to,
		}
		if delivery.ReportKind == "insights" {
			platform, platformErr := reporter.Platform(ctx, delivery.SiteID, environment, from, to)
			if platformErr != nil {
				return nil, platformErr
			}
			previousPlatform, platformErr := reporter.Platform(ctx, delivery.SiteID, environment, previousFrom, previousTo)
			if platformErr != nil {
				return nil, platformErr
			}
			data["insights"] = insight.RankInsights(platform, previousPlatform)
		}
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
		// An error count and an affected-user count, delivered under the name of
		// the screen whose subject is Web Vitals — the digest carried no vitals
		// at all, and nothing about what the errors did to conversion. An error
		// count on its own does not say whether it mattered, and a mailed report
		// has no screen beside it to be checked against.
		summary, err := insight.New(a.DB).Experience(ctx, delivery.SiteID, environment, from, to)
		if err != nil {
			return nil, err
		}
		data = map[string]any{"p75": summary.P75, "errors": summary.Errors, "affected_users": summary.AffectedUsers,
			"users": summary.Users, "error_users": summary.ErrorUsers,
			"error_user_conversion_rate": summary.ErrorUserConversionRate,
			"clean_user_conversion_rate": summary.CleanUserConversionRate,
			"conversion_rate_delta":      summary.ConversionRateDelta}
	case "ai":
		// This counted calls, users and the two token sums, which is what the AI
		// screen's query used to stop at before v0.34.3 and what the MCP tool
		// stopped at until it was fixed. The digest was the third copy, and the
		// one nobody can check: a mailed report has no chart beside it. Whoever
		// subscribes to a weekly AI digest is asking what it cost and whether it
		// worked, and the digest carried neither.
		group := insight.AIOperationDimension("")
		if value, ok := definition["group_by"].(string); ok {
			group = insight.AIOperationDimension(value)
		}
		reporter := insight.New(a.DB)
		rows, err := reporter.AIOperations(ctx, delivery.SiteID, environment, group, from, to)
		if err != nil {
			return nil, err
		}
		users, err := reporter.AIOperationUsers(ctx, delivery.SiteID, environment, from, to)
		if err != nil {
			return nil, err
		}
		totals := insight.AIOperationTotals(rows)
		totals["users"] = users
		data = map[string]any{"group_by": group, "rows": rows, "totals": totals}
	case "anomaly":
		_, location, tzErr := a.siteLocation(ctx, delivery.SiteID)
		if tzErr != nil {
			return nil, tzErr
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
		// "Segment 집계" used to mean three properties — an event name, a feature
		// and a department — and nothing else. The console's Segment builder makes
		// nested conditions, behavioural aggregates and friction rules, and none of
		// it could be delivered: the compiler lived in the HTTP package and the
		// scheduler could not reach it. So the same word named two different
		// populations depending on which door you came through, and the product's
		// "Segment → Action" was the one meaning that did not work.
		//
		// A definition that names a saved segment is now evaluated with the same
		// compiler the screens use. The three property filters stay, because
		// existing schedules use them and they are a legitimate way to ask a
		// narrow question without saving a segment first.
		eventName, _ := definition["event_name"].(string)
		feature, _ := definition["feature"].(string)
		department, _ := definition["department"].(string)
		segmentID, _ := definition["segment_id"].(string)
		args := []any{delivery.SiteID, from, to, environment, eventName, feature, department}
		predicate := ""
		segmentName := ""
		if segmentID != "" {
			node, name, loadErr := a.loadSegment(ctx, delivery.SiteID, segmentID)
			if loadErr != nil {
				return nil, loadErr
			}
			segmentName = name
			resolver, resolverErr := segment.NewResolver(ctx, a.DB, delivery.SiteID, environment)
			if resolverErr != nil {
				return nil, resolverErr
			}
			compiled, compileErr := segment.Compile(node, resolver, "e", &args, 0)
			if compileErr != nil {
				return nil, compileErr
			}
			predicate = " AND (" + compiled + ")"
		}
		where := `FROM analytics_events e WHERE e.site_id=$1 AND e.environment=$4 AND e.event_timestamp >= $2 AND e.event_timestamp < $3
			AND ($5='' OR e.event_name=$5) AND ($6='' OR e.properties->>'feature'=$6) AND ($7='' OR e.canonical_user_properties->>'department'=$7)` + predicate
		var count int64
		if err := a.DB.QueryRow(ctx, `SELECT count(DISTINCT e.entity_id) `+where, args...).Scan(&count); err != nil {
			return nil, err
		}
		data = map[string]any{"matched_entities": count, "event_name": eventName, "feature": feature, "department": department}
		if segmentID != "" {
			data["segment_id"] = segmentID
			data["segment_name"] = segmentName
		}
		if config.MaxEntityIDs > 0 {
			rows, queryErr := a.DB.Query(ctx, `SELECT DISTINCT e.entity_id `+where+` LIMIT $`+strconv.Itoa(len(args)+1), append(args, config.MaxEntityIDs)...)
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
