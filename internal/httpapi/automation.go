package httpapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/hkjang/Momento/internal/auth"
	"github.com/hkjang/Momento/internal/service"
)

func validateChannelType(value string) bool {
	switch value {
	case "webhook", "confluence", "mail", "internal_message", "ai_agent":
		return true
	default:
		return false
	}
}

func validateEndpointURL(value string) bool {
	u, err := url.Parse(value)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil
}

func (s *Server) listDeliveryChannels(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	rows, err := s.DB.Query(r.Context(), `SELECT id,name,channel_type,endpoint_url,headers,headers_secret,active,created_at,updated_at FROM delivery_channels WHERE site_id=$1 ORDER BY name`, siteID)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var name, kind, endpoint string
		var headers []byte
		var sealed *string
		var active bool
		var created, updated time.Time
		if rows.Scan(&id, &name, &kind, &endpoint, &headers, &sealed, &active, &created, &updated) == nil {
			values := map[string]any{}
			if plain, err := s.openSecret(sealed); err == nil && plain != "" {
				headers = []byte(plain)
			}
			_ = json.Unmarshal(headers, &values)
			keys := make([]string, 0, len(values))
			for key := range values {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			out = append(out, map[string]any{"id": id, "name": name, "channel_type": kind, "endpoint_url": endpoint, "header_names": keys, "has_headers": len(keys) > 0, "active": active, "created_at": created, "updated_at": updated})
		}
	}
	writeJSON(w, 200, out)
}

func (s *Server) saveDeliveryChannel(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	siteID, siteErr := s.resolveSite(r, "siteID")
	if siteErr != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	var in struct {
		ID          string            `json:"id"`
		Name        string            `json:"name"`
		ChannelType string            `json:"channel_type"`
		EndpointURL string            `json:"endpoint_url"`
		Headers     map[string]string `json:"headers"`
		Active      *bool             `json:"active"`
	}
	if err := decodeJSON(r, &in, 64<<10); err != nil {
		writeError(w, 400, "INVALID_PAYLOAD", err.Error())
		return
	}
	if strings.TrimSpace(in.Name) == "" || !validateChannelType(in.ChannelType) || !validateEndpointURL(in.EndpointURL) {
		writeError(w, 400, "INVALID_CHANNEL", "name, supported channel_type and http(s) endpoint_url are required")
		return
	}
	for key, value := range in.Headers {
		if key == "" || strings.EqualFold(key, "Host") || strings.ContainsAny(key+value, "\r\n") {
			writeError(w, 400, "INVALID_HEADERS", "header names and values must be valid and Host cannot be overridden")
			return
		}
	}
	active := true
	if in.Active != nil {
		active = *in.Active
	}
	headers, _ := json.Marshal(in.Headers)
	// Credentials are sealed when an encryption key is configured, so the plain
	// JSON column only keeps values written before encryption was enabled.
	stored, sealed := headers, s.sealSecret(string(headers))
	if sealed != nil {
		stored = []byte("{}")
	}
	var id uuid.UUID
	var err error
	if in.ID == "" {
		err = s.DB.QueryRow(r.Context(), `INSERT INTO delivery_channels(site_id,name,channel_type,endpoint_url,headers,headers_secret,active,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, siteID, in.Name, in.ChannelType, in.EndpointURL, stored, sealed, active, p.ID).Scan(&id)
	} else {
		id, err = uuid.Parse(in.ID)
		if err == nil {
			if in.Headers == nil {
				_, err = s.DB.Exec(r.Context(), `UPDATE delivery_channels SET name=$3,channel_type=$4,endpoint_url=$5,active=$6,updated_at=now() WHERE id=$1 AND site_id=$2`, id, siteID, in.Name, in.ChannelType, in.EndpointURL, active)
			} else {
				_, err = s.DB.Exec(r.Context(), `UPDATE delivery_channels SET name=$3,channel_type=$4,endpoint_url=$5,headers=$6,headers_secret=$7,active=$8,updated_at=now() WHERE id=$1 AND site_id=$2`, id, siteID, in.Name, in.ChannelType, in.EndpointURL, stored, sealed, active)
			}
		}
	}
	if err != nil {
		writeError(w, 500, "CHANNEL_SAVE_FAILED", err.Error())
		return
	}
	s.audit(r.Context(), &p, "delivery_channel.save", "delivery_channel", id.String(), map[string]any{"name": in.Name, "type": in.ChannelType}, clientIP(r))
	writeJSON(w, 200, map[string]any{"id": id, "headers_stored": in.Headers != nil})
}

func (s *Server) deleteDeliveryChannel(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	siteID, siteErr := s.resolveSite(r, "siteID")
	if siteErr != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "INVALID_ID", "invalid channel id")
		return
	}
	tag, err := s.DB.Exec(r.Context(), `DELETE FROM delivery_channels WHERE id=$1 AND site_id=$2`, id, siteID)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, 404, "NOT_FOUND", "channel not found or is in use")
		return
	}
	s.audit(r.Context(), &p, "delivery_channel.delete", "delivery_channel", id.String(), nil, clientIP(r))
	w.WriteHeader(204)
}

func (s *Server) listScheduledReports(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	rows, err := s.DB.Query(r.Context(), `SELECT r.id,r.channel_id,c.name,r.name,r.report_kind,r.definition,r.interval_minutes,r.next_run_at,r.enabled,r.last_run_at,r.last_status,r.last_error,r.created_at,r.updated_at FROM scheduled_reports r JOIN delivery_channels c ON c.id=r.channel_id AND c.site_id=r.site_id WHERE r.site_id=$1 ORDER BY r.created_at DESC`, siteID)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, channelID uuid.UUID
		var channelName, name, kind string
		var definition []byte
		var interval int
		var next, created, updated time.Time
		var enabled bool
		var lastRun *time.Time
		var status, lastError *string
		if rows.Scan(&id, &channelID, &channelName, &name, &kind, &definition, &interval, &next, &enabled, &lastRun, &status, &lastError, &created, &updated) == nil {
			var value any
			_ = json.Unmarshal(definition, &value)
			out = append(out, map[string]any{"id": id, "channel_id": channelID, "channel_name": channelName, "name": name, "report_kind": kind, "definition": value, "interval_minutes": interval, "next_run_at": next, "enabled": enabled, "last_run_at": lastRun, "last_status": status, "last_error": lastError, "created_at": created, "updated_at": updated})
		}
	}
	writeJSON(w, 200, out)
}

func validReportKind(value string) bool {
	switch value {
	case "overview", "adoption", "experience", "ai", "segment", "insights":
		return true
	default:
		return false
	}
}

func (s *Server) saveScheduledReport(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	var in struct {
		ID              string         `json:"id"`
		ChannelID       string         `json:"channel_id"`
		Name            string         `json:"name"`
		ReportKind      string         `json:"report_kind"`
		Definition      map[string]any `json:"definition"`
		IntervalMinutes int            `json:"interval_minutes"`
		NextRunAt       *time.Time     `json:"next_run_at"`
		Enabled         *bool          `json:"enabled"`
	}
	if err := decodeJSON(r, &in, 128<<10); err != nil {
		writeError(w, 400, "INVALID_PAYLOAD", err.Error())
		return
	}
	channelID, err := uuid.Parse(in.ChannelID)
	if err != nil || strings.TrimSpace(in.Name) == "" || !validReportKind(in.ReportKind) || in.IntervalMinutes < 5 || in.IntervalMinutes > 525600 {
		writeError(w, 400, "INVALID_SCHEDULE", "channel, name, supported report_kind and interval_minutes between 5 and 525600 are required")
		return
	}
	var channelExists bool
	_ = s.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM delivery_channels WHERE id=$1 AND site_id=$2 AND active)`, channelID, siteID).Scan(&channelExists)
	if !channelExists {
		writeError(w, 400, "INVALID_CHANNEL", "channel is not active for this site")
		return
	}
	if environment, ok := in.Definition["environment"].(string); ok && environment != "" && !environmentNamePattern.MatchString(environment) {
		writeError(w, 400, "INVALID_ENVIRONMENT", "invalid definition environment")
		return
	}
	definition, _ := json.Marshal(in.Definition)
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	nextRun := time.Now()
	if in.NextRunAt != nil {
		nextRun = *in.NextRunAt
	}
	var id uuid.UUID
	if in.ID == "" {
		err = s.DB.QueryRow(r.Context(), `INSERT INTO scheduled_reports(site_id,channel_id,name,report_kind,definition,interval_minutes,next_run_at,enabled,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`, siteID, channelID, in.Name, in.ReportKind, definition, in.IntervalMinutes, nextRun, enabled, p.ID).Scan(&id)
	} else {
		id, err = uuid.Parse(in.ID)
		if err == nil {
			_, err = s.DB.Exec(r.Context(), `UPDATE scheduled_reports SET channel_id=$3,name=$4,report_kind=$5,definition=$6,interval_minutes=$7,next_run_at=$8,enabled=$9,updated_at=now() WHERE id=$1 AND site_id=$2`, id, siteID, channelID, in.Name, in.ReportKind, definition, in.IntervalMinutes, nextRun, enabled)
		}
	}
	if err != nil {
		writeError(w, 500, "SCHEDULE_SAVE_FAILED", err.Error())
		return
	}
	s.audit(r.Context(), &p, "scheduled_report.save", "scheduled_report", id.String(), map[string]any{"name": in.Name, "kind": in.ReportKind}, clientIP(r))
	writeJSON(w, 200, map[string]any{"id": id})
}

func (s *Server) deleteScheduledReport(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	siteID, siteErr := s.resolveSite(r, "siteID")
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if siteErr != nil || err != nil {
		writeError(w, 404, "NOT_FOUND", "schedule not found")
		return
	}
	tag, err := s.DB.Exec(r.Context(), `DELETE FROM scheduled_reports WHERE id=$1 AND site_id=$2`, id, siteID)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, 404, "NOT_FOUND", "schedule not found")
		return
	}
	s.audit(r.Context(), &p, "scheduled_report.delete", "scheduled_report", id.String(), nil, clientIP(r))
	w.WriteHeader(204)
}

func (s *Server) runScheduledReport(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	siteID, siteErr := s.resolveSite(r, "siteID")
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if siteErr != nil || err != nil {
		writeError(w, 404, "NOT_FOUND", "schedule not found")
		return
	}
	var exists bool
	_ = s.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM scheduled_reports WHERE id=$1 AND site_id=$2)`, id, siteID).Scan(&exists)
	if !exists {
		writeError(w, 404, "NOT_FOUND", "schedule not found")
		return
	}
	if err := (service.Automation{DB: s.DB, Logger: s.Logger}).RunByID(r.Context(), id); err != nil {
		writeError(w, 502, "DELIVERY_FAILED", err.Error())
		return
	}
	s.audit(r.Context(), &p, "scheduled_report.run", "scheduled_report", id.String(), nil, clientIP(r))
	writeJSON(w, 200, map[string]bool{"delivered": true})
}

func (s *Server) listDeliveryRuns(w http.ResponseWriter, r *http.Request) {
	siteID, siteErr := s.resolveSite(r, "siteID")
	if siteErr != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	rows, err := s.DB.Query(r.Context(), `SELECT id,report_id,channel_id,status,response_status,error,started_at,finished_at FROM delivery_runs WHERE site_id=$1 ORDER BY started_at DESC LIMIT 500`, siteID)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int64
		var reportID, channelID *uuid.UUID
		var status string
		var responseStatus *int
		var errorText *string
		var started time.Time
		var finished *time.Time
		if rows.Scan(&id, &reportID, &channelID, &status, &responseStatus, &errorText, &started, &finished) == nil {
			out = append(out, map[string]any{"id": id, "report_id": reportID, "channel_id": channelID, "status": status, "response_status": responseStatus, "error": errorText, "started_at": started, "finished_at": finished})
		}
	}
	writeJSON(w, 200, out)
}
