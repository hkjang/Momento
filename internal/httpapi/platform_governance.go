package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/hkjang/Momento/internal/auth"
)

var registryNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
var environmentNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

func requestEnvironment(r *http.Request) string {
	value := strings.TrimSpace(r.URL.Query().Get("environment"))
	if value == "" {
		return "prd"
	}
	if !environmentNamePattern.MatchString(value) {
		return "prd"
	}
	return value
}

func (s *Server) listEnvironments(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	rows, err := s.DB.Query(r.Context(), `SELECT name,label,contract_mode,cardinality_limit,active,created_at,updated_at FROM site_environments WHERE site_id=$1 ORDER BY CASE name WHEN 'prd' THEN 1 WHEN 'stg' THEN 2 WHEN 'dev' THEN 3 ELSE 4 END,name`, siteID)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var name, label, mode string
		var limit int
		var active bool
		var created, updated time.Time
		if err := rows.Scan(&name, &label, &mode, &limit, &active, &created, &updated); err != nil {
			writeError(w, 500, "QUERY_FAILED", err.Error())
			return
		}
		out = append(out, map[string]any{"name": name, "label": label, "contract_mode": mode, "cardinality_limit": limit, "active": active, "created_at": created, "updated_at": updated})
	}
	if err := rows.Err(); err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	writeJSON(w, 200, out)
}

func (s *Server) putEnvironment(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	name := strings.TrimSpace(chi.URLParam(r, "name"))
	if !environmentNamePattern.MatchString(name) {
		writeError(w, 400, "INVALID_ENVIRONMENT", "environment must use lowercase letters, numbers, underscore or dash")
		return
	}
	var in struct {
		Label            string `json:"label"`
		ContractMode     string `json:"contract_mode"`
		CardinalityLimit int    `json:"cardinality_limit"`
		Active           *bool  `json:"active"`
	}
	if err := decodeJSON(r, &in, 32<<10); err != nil {
		writeError(w, 400, "INVALID_PAYLOAD", err.Error())
		return
	}
	if strings.TrimSpace(in.Label) == "" {
		in.Label = name
	}
	if in.ContractMode != "allow" && in.ContractMode != "warn" && in.ContractMode != "reject" {
		writeError(w, 400, "INVALID_CONTRACT_MODE", "contract_mode must be allow, warn or reject")
		return
	}
	if in.CardinalityLimit < 100 || in.CardinalityLimit > 10000000 {
		writeError(w, 400, "INVALID_CARDINALITY_LIMIT", "cardinality_limit must be between 100 and 10000000")
		return
	}
	active := true
	if in.Active != nil {
		active = *in.Active
	}
	if name == "prd" && !active {
		writeError(w, 400, "PRODUCTION_REQUIRED", "prd environment cannot be disabled")
		return
	}
	_, err = s.DB.Exec(r.Context(), `INSERT INTO site_environments(site_id,name,label,contract_mode,cardinality_limit,active) VALUES($1,$2,$3,$4,$5,$6)
		ON CONFLICT(site_id,name) DO UPDATE SET label=excluded.label,contract_mode=excluded.contract_mode,cardinality_limit=excluded.cardinality_limit,active=excluded.active,updated_at=now()`, siteID, name, in.Label, in.ContractMode, in.CardinalityLimit, active)
	if err != nil {
		writeError(w, 500, "ENVIRONMENT_SAVE_FAILED", err.Error())
		return
	}
	s.audit(r.Context(), &p, "environment.update", "site_environment", siteID.String()+":"+name, map[string]any{"contract_mode": in.ContractMode, "cardinality_limit": in.CardinalityLimit, "active": active}, clientIP(r))
	writeJSON(w, 200, map[string]bool{"updated": true})
}

func (s *Server) listEventContracts(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	rows, err := s.DB.Query(r.Context(), `SELECT d.name,d.description,d.owner,d.current_version,d.deprecated,v.version,v.schema,v.validation_mode,v.status,v.changelog,v.created_at,v.activated_at
		FROM event_definitions d JOIN event_contract_versions v ON v.site_id=d.site_id AND v.event_name=d.name WHERE d.site_id=$1 ORDER BY d.name,v.version DESC`, siteID)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var name, description, owner, validation, status, changelog string
		var current, version int
		var deprecated bool
		var schema []byte
		var created time.Time
		var activated *time.Time
		if err := rows.Scan(&name, &description, &owner, &current, &deprecated, &version, &schema, &validation, &status, &changelog, &created, &activated); err != nil {
			writeError(w, 500, "QUERY_FAILED", err.Error())
			return
		}
		var schemaValue any
		_ = json.Unmarshal(schema, &schemaValue)
		out = append(out, map[string]any{"event_name": name, "description": description, "owner": owner, "current_version": current, "deprecated": deprecated, "version": version, "schema": schemaValue, "validation_mode": validation, "status": status, "changelog": changelog, "created_at": created, "activated_at": activated})
	}
	if err := rows.Err(); err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	writeJSON(w, 200, out)
}

func (s *Server) createEventContract(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	var in struct {
		EventName      string         `json:"event_name"`
		Description    string         `json:"description"`
		Owner          string         `json:"owner"`
		Schema         map[string]any `json:"schema"`
		ValidationMode string         `json:"validation_mode"`
		Changelog      string         `json:"changelog"`
		Activate       bool           `json:"activate"`
	}
	if err := decodeJSON(r, &in, 256<<10); err != nil {
		writeError(w, 400, "INVALID_PAYLOAD", err.Error())
		return
	}
	if !eventNamePatternForProperty.MatchString(in.EventName) || len(in.EventName) > 64 {
		writeError(w, 400, "INVALID_EVENT", "invalid event name")
		return
	}
	if in.ValidationMode == "" {
		in.ValidationMode = "warn"
	}
	if in.ValidationMode != "allow" && in.ValidationMode != "warn" && in.ValidationMode != "reject" {
		writeError(w, 400, "INVALID_VALIDATION_MODE", "validation_mode must be allow, warn or reject")
		return
	}
	schema, _ := json.Marshal(in.Schema)
	tx, err := s.DB.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "CONTRACT_SAVE_FAILED", err.Error())
		return
	}
	defer tx.Rollback(r.Context())
	_, err = tx.Exec(r.Context(), `INSERT INTO event_definitions(site_id,name,description,schema,validation_mode,owner) VALUES($1,$2,$3,$4,$5,$6)
		ON CONFLICT(site_id,name) DO UPDATE SET description=excluded.description,owner=excluded.owner,updated_at=now()`, siteID, in.EventName, in.Description, schema, in.ValidationMode, in.Owner)
	if err != nil {
		writeError(w, 500, "CONTRACT_SAVE_FAILED", err.Error())
		return
	}
	var version int
	if err := tx.QueryRow(r.Context(), `SELECT coalesce(max(version),0)+1 FROM event_contract_versions WHERE site_id=$1 AND event_name=$2`, siteID, in.EventName).Scan(&version); err != nil {
		writeError(w, 500, "CONTRACT_SAVE_FAILED", err.Error())
		return
	}
	status := "draft"
	var activated any
	if in.Activate {
		status, activated = "active", time.Now()
		_, _ = tx.Exec(r.Context(), `UPDATE event_contract_versions SET status='deprecated' WHERE site_id=$1 AND event_name=$2 AND status='active'`, siteID, in.EventName)
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO event_contract_versions(site_id,event_name,version,schema,validation_mode,status,changelog,created_by,activated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, siteID, in.EventName, version, schema, in.ValidationMode, status, in.Changelog, p.ID, activated)
	if err == nil && in.Activate {
		_, err = tx.Exec(r.Context(), `UPDATE event_definitions SET current_version=$3,schema=$4,validation_mode=$5,deprecated=false,updated_at=now() WHERE site_id=$1 AND name=$2`, siteID, in.EventName, version, schema, in.ValidationMode)
	}
	if err != nil {
		writeError(w, 500, "CONTRACT_SAVE_FAILED", err.Error())
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "CONTRACT_SAVE_FAILED", err.Error())
		return
	}
	s.audit(r.Context(), &p, "event_contract.create", "event_contract", fmt.Sprintf("%s:%d", in.EventName, version), map[string]any{"active": in.Activate}, clientIP(r))
	writeJSON(w, 201, map[string]any{"event_name": in.EventName, "version": version, "status": status})
}

func (s *Server) activateEventContract(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	eventName := chi.URLParam(r, "eventName")
	version, err := parsePositiveInt(chi.URLParam(r, "version"))
	if err != nil {
		writeError(w, 400, "INVALID_VERSION", "version must be a positive integer")
		return
	}
	tx, err := s.DB.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "CONTRACT_ACTIVATE_FAILED", err.Error())
		return
	}
	defer tx.Rollback(r.Context())
	var schema []byte
	var mode string
	if err := tx.QueryRow(r.Context(), `SELECT schema,validation_mode FROM event_contract_versions WHERE site_id=$1 AND event_name=$2 AND version=$3 FOR UPDATE`, siteID, eventName, version).Scan(&schema, &mode); err != nil {
		writeError(w, 404, "CONTRACT_NOT_FOUND", "event contract version not found")
		return
	}
	_, _ = tx.Exec(r.Context(), `UPDATE event_contract_versions SET status='deprecated' WHERE site_id=$1 AND event_name=$2 AND status='active'`, siteID, eventName)
	if _, err := tx.Exec(r.Context(), `UPDATE event_contract_versions SET status='active',activated_at=now() WHERE site_id=$1 AND event_name=$2 AND version=$3`, siteID, eventName, version); err != nil {
		writeError(w, 500, "CONTRACT_ACTIVATE_FAILED", err.Error())
		return
	}
	if _, err := tx.Exec(r.Context(), `UPDATE event_definitions SET current_version=$3,schema=$4,validation_mode=$5,deprecated=false,updated_at=now() WHERE site_id=$1 AND name=$2`, siteID, eventName, version, schema, mode); err != nil {
		writeError(w, 500, "CONTRACT_ACTIVATE_FAILED", err.Error())
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "CONTRACT_ACTIVATE_FAILED", err.Error())
		return
	}
	s.audit(r.Context(), &p, "event_contract.activate", "event_contract", fmt.Sprintf("%s:%d", eventName, version), nil, clientIP(r))
	writeJSON(w, 200, map[string]bool{"activated": true})
}

func parsePositiveInt(raw string) (int, error) {
	var value int
	_, err := fmt.Sscan(raw, &value)
	if err != nil || value < 1 {
		return 0, fmt.Errorf("invalid positive integer")
	}
	return value, nil
}

type semanticDefinition struct {
	Type             string              `json:"type"`
	Metric           string              `json:"metric,omitempty"`
	EventName        string              `json:"event_name,omitempty"`
	Conversion       *bool               `json:"conversion,omitempty"`
	Property         string              `json:"property,omitempty"`
	FallbackProperty string              `json:"fallback_property,omitempty"`
	TrafficClass     string              `json:"traffic_class,omitempty"`
	MinOccurrences   int                 `json:"min_occurrences,omitempty"`
	Filters          []semanticFilter    `json:"filters,omitempty"`
	Numerator        *semanticDefinition `json:"numerator,omitempty"`
	Denominator      *semanticDefinition `json:"denominator,omitempty"`
}

type semanticFilter struct {
	Property string `json:"property"`
	Operator string `json:"operator"`
	Value    any    `json:"value,omitempty"`
	Scope    string `json:"scope,omitempty"`
}

func validateSemanticDefinition(def semanticDefinition, depth int) error {
	if depth > 5 {
		return fmt.Errorf("metric definition nesting is limited to 5")
	}
	if def.EventName != "" && (!eventNamePatternForProperty.MatchString(def.EventName) || len(def.EventName) > 64) {
		return fmt.Errorf("invalid event_name")
	}
	if def.Property != "" && !eventNamePatternForProperty.MatchString(def.Property) {
		return fmt.Errorf("invalid property")
	}
	if def.FallbackProperty != "" && !eventNamePatternForProperty.MatchString(def.FallbackProperty) {
		return fmt.Errorf("invalid fallback_property")
	}
	if def.TrafficClass != "" && !map[string]bool{"normal": true, "internal_traffic": true, "known_bot": true, "monitoring": true, "suspicious": true}[def.TrafficClass] {
		return fmt.Errorf("invalid traffic_class")
	}
	if def.MinOccurrences < 0 || def.MinOccurrences > 1000000 {
		return fmt.Errorf("min_occurrences must be between 0 and 1000000")
	}
	for _, filter := range def.Filters {
		if !eventNamePatternForProperty.MatchString(filter.Property) {
			return fmt.Errorf("invalid filter property")
		}
		if !map[string]bool{"eq": true, "neq": true, "exists": true, "not_exists": true, "in": true}[filter.Operator] {
			return fmt.Errorf("unsupported filter operator")
		}
		if filter.Scope != "" && filter.Scope != "event" && filter.Scope != "user" && filter.Scope != "session" {
			return fmt.Errorf("filter scope must be event, user or session")
		}
		if filter.Operator == "in" {
			values, ok := filter.Value.([]any)
			if !ok || len(values) == 0 || len(values) > 100 {
				return fmt.Errorf("in filter requires 1 to 100 values")
			}
		}
	}
	switch def.Type {
	case "count", "unique_users", "unique_sessions":
		return nil
	case "sum", "average":
		if def.Property == "" {
			return fmt.Errorf("property is required for %s", def.Type)
		}
		return nil
	case "ratio":
		if def.Numerator == nil || def.Denominator == nil {
			return fmt.Errorf("ratio requires numerator and denominator")
		}
		if err := validateSemanticDefinition(*def.Numerator, depth+1); err != nil {
			return err
		}
		return validateSemanticDefinition(*def.Denominator, depth+1)
	case "metric_ref":
		if !registryNamePattern.MatchString(def.Metric) {
			return fmt.Errorf("metric_ref requires a valid metric")
		}
		return nil
	default:
		return fmt.Errorf("unsupported metric type")
	}
}

func (s *Server) validateMetricReferences(ctx context.Context, siteID uuid.UUID, metricName string, def semanticDefinition, seen map[string]bool, depth int) error {
	if depth > 5 {
		return fmt.Errorf("metric reference nesting is limited to 5")
	}
	if def.Type == "ratio" {
		if err := s.validateMetricReferences(ctx, siteID, metricName, *def.Numerator, seen, depth+1); err != nil {
			return err
		}
		return s.validateMetricReferences(ctx, siteID, metricName, *def.Denominator, seen, depth+1)
	}
	if def.Type != "metric_ref" {
		return nil
	}
	if def.Metric == metricName || seen[def.Metric] {
		return fmt.Errorf("metric reference cycle detected at %s", def.Metric)
	}
	var raw []byte
	if err := s.DB.QueryRow(ctx, `SELECT definition FROM semantic_metrics WHERE site_id=$1 AND name=$2 AND status='active'`, siteID, def.Metric).Scan(&raw); err != nil {
		return fmt.Errorf("referenced metric %q not found", def.Metric)
	}
	var referenced semanticDefinition
	if err := json.Unmarshal(raw, &referenced); err != nil {
		return fmt.Errorf("referenced metric %q has an invalid definition", def.Metric)
	}
	nextSeen := make(map[string]bool, len(seen)+1)
	for name, value := range seen {
		nextSeen[name] = value
	}
	nextSeen[def.Metric] = true
	return s.validateMetricReferences(ctx, siteID, metricName, referenced, nextSeen, depth+1)
}

func (s *Server) listSemanticMetrics(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	rows, err := s.DB.Query(r.Context(), `SELECT id,name,label,description,definition,format,unit,definition_version,status,owner,entity_scope,tags,created_at,updated_at FROM semantic_metrics WHERE site_id=$1 ORDER BY name`, siteID)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var name, label, description, format, unit, status, owner, entityScope string
		var tags []string
		var definition []byte
		var version int
		var created, updated time.Time
		if err := rows.Scan(&id, &name, &label, &description, &definition, &format, &unit, &version, &status, &owner, &entityScope, &tags, &created, &updated); err != nil {
			writeError(w, 500, "QUERY_FAILED", err.Error())
			return
		}
		var value any
		_ = json.Unmarshal(definition, &value)
		out = append(out, map[string]any{"id": id, "name": name, "label": label, "description": description, "definition": value, "format": format, "unit": unit, "definition_version": version, "status": status, "owner": owner, "entity_scope": entityScope, "tags": tags, "created_at": created, "updated_at": updated})
	}
	if err := rows.Err(); err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	writeJSON(w, 200, out)
}

func (s *Server) saveSemanticMetric(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	var in struct {
		Name        string             `json:"name"`
		Label       string             `json:"label"`
		Description string             `json:"description"`
		Definition  semanticDefinition `json:"definition"`
		Format      string             `json:"format"`
		Unit        string             `json:"unit"`
		Status      string             `json:"status"`
		Owner       string             `json:"owner"`
		EntityScope string             `json:"entity_scope"`
		Tags        []string           `json:"tags"`
	}
	if err := decodeJSON(r, &in, 128<<10); err != nil {
		writeError(w, 400, "INVALID_PAYLOAD", err.Error())
		return
	}
	if !registryNamePattern.MatchString(in.Name) || strings.TrimSpace(in.Label) == "" {
		writeError(w, 400, "INVALID_METRIC", "valid name and label are required")
		return
	}
	if err := validateSemanticDefinition(in.Definition, 1); err != nil {
		writeError(w, 400, "INVALID_METRIC_DEFINITION", err.Error())
		return
	}
	if err := s.validateMetricReferences(r.Context(), siteID, in.Name, in.Definition, map[string]bool{in.Name: true}, 1); err != nil {
		writeError(w, 400, "INVALID_METRIC_REFERENCE", err.Error())
		return
	}
	if in.Format == "" {
		in.Format = "number"
	}
	if in.Format != "number" && in.Format != "percent" && in.Format != "duration" && in.Format != "currency" {
		writeError(w, 400, "INVALID_FORMAT", "format must be number, percent, duration or currency")
		return
	}
	if in.Status == "" {
		in.Status = "active"
	}
	if in.Status != "active" && in.Status != "deprecated" {
		writeError(w, 400, "INVALID_STATUS", "status must be active or deprecated")
		return
	}
	if in.EntityScope == "" {
		in.EntityScope = "event"
	}
	if !map[string]bool{"event": true, "session": true, "user": true, "item": true}[in.EntityScope] {
		writeError(w, 400, "INVALID_SCOPE", "entity_scope must be event, session, user or item")
		return
	}
	if len(in.Tags) > 20 {
		writeError(w, 400, "INVALID_TAGS", "at most 20 tags are allowed")
		return
	}
	if in.Tags == nil {
		in.Tags = []string{}
	}
	for _, tag := range in.Tags {
		if strings.TrimSpace(tag) == "" || len(tag) > 64 {
			writeError(w, 400, "INVALID_TAGS", "tags must be non-empty and at most 64 characters")
			return
		}
	}
	definition, _ := json.Marshal(in.Definition)
	var id uuid.UUID
	var version int
	err = s.DB.QueryRow(r.Context(), `INSERT INTO semantic_metrics(site_id,name,label,description,definition,format,unit,status,owner,entity_scope,tags,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT(site_id,name) DO UPDATE SET label=excluded.label,description=excluded.description,definition=excluded.definition,format=excluded.format,unit=excluded.unit,status=excluded.status,owner=excluded.owner,entity_scope=excluded.entity_scope,tags=excluded.tags,definition_version=semantic_metrics.definition_version+1,updated_at=now()
		RETURNING id,definition_version`, siteID, in.Name, in.Label, in.Description, definition, in.Format, in.Unit, in.Status, in.Owner, in.EntityScope, in.Tags, p.ID).Scan(&id, &version)
	if err != nil {
		writeError(w, 500, "METRIC_SAVE_FAILED", err.Error())
		return
	}
	s.audit(r.Context(), &p, "semantic_metric.save", "semantic_metric", id.String(), map[string]any{"name": in.Name, "version": version}, clientIP(r))
	writeJSON(w, 200, map[string]any{"id": id, "definition_version": version})
}

func (s *Server) querySemanticMetric(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	from, to, err := s.dateRange(r, siteID)
	if err != nil {
		writeRangeError(w, err)
		return
	}
	name := chi.URLParam(r, "name")
	var raw []byte
	var label, format, unit string
	var version int
	if err := s.DB.QueryRow(r.Context(), `SELECT definition,label,format,unit,definition_version FROM semantic_metrics WHERE site_id=$1 AND name=$2 AND status='active'`, siteID, name).Scan(&raw, &label, &format, &unit, &version); err != nil {
		writeError(w, 404, "METRIC_NOT_FOUND", "semantic metric not found")
		return
	}
	var definition semanticDefinition
	if err := json.Unmarshal(raw, &definition); err != nil {
		writeError(w, 500, "INVALID_METRIC_DEFINITION", err.Error())
		return
	}
	value, err := s.evaluateSemanticMetric(r, siteID, requestEnvironment(r), from, to, definition, 1)
	if err != nil {
		writeError(w, 500, "METRIC_QUERY_FAILED", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"name": name, "label": label, "value": value, "format": format, "unit": unit, "definition_version": version, "environment": requestEnvironment(r), "from": from, "to": to})
}

func (s *Server) evaluateSemanticMetric(r *http.Request, siteID uuid.UUID, environment string, from, to time.Time, def semanticDefinition, depth int) (float64, error) {
	if err := validateSemanticDefinition(def, depth); err != nil {
		return 0, err
	}
	if def.Type == "ratio" {
		numeratorDefinition := *def.Numerator
		denominatorDefinition := *def.Denominator
		numeratorDefinition.Filters = append(numeratorDefinition.Filters, def.Filters...)
		denominatorDefinition.Filters = append(denominatorDefinition.Filters, def.Filters...)
		numerator, err := s.evaluateSemanticMetric(r, siteID, environment, from, to, numeratorDefinition, depth+1)
		if err != nil {
			return 0, err
		}
		denominator, err := s.evaluateSemanticMetric(r, siteID, environment, from, to, denominatorDefinition, depth+1)
		if err != nil || denominator == 0 {
			return 0, err
		}
		return numerator / denominator * 100, nil
	}
	if def.Type == "metric_ref" {
		var raw []byte
		if err := s.DB.QueryRow(r.Context(), `SELECT definition FROM semantic_metrics WHERE site_id=$1 AND name=$2 AND status='active'`, siteID, def.Metric).Scan(&raw); err != nil {
			return 0, fmt.Errorf("referenced metric %q not found", def.Metric)
		}
		var referenced semanticDefinition
		if err := json.Unmarshal(raw, &referenced); err != nil {
			return 0, err
		}
		referenced.Filters = append(referenced.Filters, def.Filters...)
		return s.evaluateSemanticMetric(r, siteID, environment, from, to, referenced, depth+1)
	}
	where := `site_id=$1 AND event_timestamp >= $2 AND event_timestamp < $3 AND environment=$4`
	args := []any{siteID, from, to, environment}
	if def.EventName != "" {
		args = append(args, def.EventName)
		where += fmt.Sprintf(" AND event_name=$%d", len(args))
	}
	if def.Conversion != nil {
		args = append(args, *def.Conversion)
		where += fmt.Sprintf(" AND is_conversion=$%d", len(args))
	}
	if def.TrafficClass != "" {
		args = append(args, def.TrafficClass)
		// client_class so a saved metric means the same thing either side of
		// v0.34.33, where traffic_class stopped carrying the network fact.
		where += fmt.Sprintf(" AND client_class=$%d", len(args))
	}
	for _, filter := range def.Filters {
		args = append(args, filter.Property)
		propertyPosition := len(args)
		propertySource := "properties"
		switch filter.Scope {
		case "user":
			propertySource = "canonical_user_properties"
		case "session":
			propertySource = `(SELECT sm.session_properties FROM sessions sm WHERE sm.site_id=analytics_events.site_id AND sm.environment=analytics_events.environment AND sm.session_id=analytics_events.session_id LIMIT 1)`
		}
		switch filter.Operator {
		case "exists":
			where += fmt.Sprintf(" AND %s ? $%d", propertySource, propertyPosition)
		case "not_exists":
			where += fmt.Sprintf(" AND NOT %s ? $%d", propertySource, propertyPosition)
		case "eq", "neq":
			args = append(args, fmt.Sprint(filter.Value))
			op := "="
			if filter.Operator == "neq" {
				op = "<>"
			}
			where += fmt.Sprintf(" AND %s->>$%d %s $%d", propertySource, propertyPosition, op, len(args))
		case "in":
			values := []string{}
			for _, value := range filter.Value.([]any) {
				values = append(values, fmt.Sprint(value))
			}
			args = append(args, values)
			where += fmt.Sprintf(" AND %s->>$%d=ANY($%d)", propertySource, propertyPosition, len(args))
		}
	}
	if def.MinOccurrences > 1 && (def.Type == "unique_users" || def.Type == "unique_sessions") {
		groupField := "entity_id"
		if def.Type == "unique_sessions" {
			groupField = "session_id"
		}
		args = append(args, def.MinOccurrences)
		var value float64
		err := s.DB.QueryRow(r.Context(), `SELECT count(*)::double precision FROM (SELECT `+groupField+` FROM analytics_events WHERE `+where+` GROUP BY `+groupField+` HAVING count(*) >= $`+fmt.Sprint(len(args))+`) repeated`, args...).Scan(&value)
		return value, err
	}
	expression := "count(*)::double precision"
	switch def.Type {
	case "unique_users":
		expression = "count(DISTINCT entity_id)::double precision"
	case "unique_sessions":
		expression = "count(DISTINCT session_id)::double precision"
	case "sum", "average":
		args = append(args, def.Property)
		propertyPosition := len(args)
		args = append(args, def.FallbackProperty)
		fallbackPosition := len(args)
		numeric := fmt.Sprintf("CASE WHEN coalesce(properties->>$%d,CASE WHEN $%d<>'' THEN properties->>$%d END,'') ~ '^-?[0-9]+(\\.[0-9]+)?$' THEN coalesce(properties->>$%d,CASE WHEN $%d<>'' THEN properties->>$%d END)::numeric END", propertyPosition, fallbackPosition, fallbackPosition, propertyPosition, fallbackPosition, fallbackPosition)
		if def.Type == "sum" {
			expression = "coalesce(sum(" + numeric + "),0)::double precision"
		} else {
			expression = "coalesce(avg(" + numeric + "),0)::double precision"
		}
	}
	var value float64
	err := s.DB.QueryRow(r.Context(), `SELECT `+expression+` FROM analytics_events WHERE `+where, args...).Scan(&value)
	return value, err
}

func (s *Server) dataQualityReport(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	from, to, err := s.dateRange(r, siteID)
	if err != nil {
		writeRangeError(w, err)
		return
	}
	environment := requestEnvironment(r)
	_, location, _ := s.siteTimezone(r.Context(), siteID)
	dateFrom := from.In(location).Format("2006-01-02")
	dateTo := to.In(location).Format("2006-01-02")
	var received, accepted, duplicates, warnings, rejected, late, missingUser, refusedUser, missingFeature, unknownNetwork, piiBlocked, piiDetected, cardinality int64
	err = s.DB.QueryRow(r.Context(), `SELECT coalesce(sum(received),0),coalesce(sum(accepted),0),coalesce(sum(duplicates),0),coalesce(sum(warnings),0),coalesce(sum(rejected),0),coalesce(sum(late_events),0),coalesce(sum(missing_user_id),0),coalesce(sum(refused_user_id),0),coalesce(sum(missing_feature),0),coalesce(sum(unknown_network),0),coalesce(sum(pii_blocked),0),coalesce(sum(pii_detected),0),coalesce(sum(cardinality_violations),0)
		FROM data_quality_daily WHERE site_id=$1 AND environment=$2 AND event_date >= $3::date AND event_date < $4::date`, siteID, environment, dateFrom, dateTo).Scan(&received, &accepted, &duplicates, &warnings, &rejected, &late, &missingUser, &refusedUser, &missingFeature, &unknownNetwork, &piiBlocked, &piiDetected, &cardinality)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	defects := duplicates + warnings + rejected + late + cardinality
	score := 100.0
	if received > 0 {
		score -= minFloat(100, float64(defects)*100/float64(received))
	}
	// The backlog and the dead letters are why this screen exists: an operator
	// reads them to decide whether ingestion is healthy. A discarded failure
	// reports no backlog and no dead letters — the screen says everything is
	// fine, which is the one answer it must never give by default.
	var inboxLag float64
	var pending, deadLetters int64
	if err := s.DB.QueryRow(r.Context(), `SELECT count(*),coalesce(extract(epoch FROM(now()-min(created_at))),0) FROM event_inbox WHERE site_id=$1 AND processed_at IS NULL`, siteID).Scan(&pending, &inboxLag); err != nil {
		writeQueryError(w, err)
		return
	}
	if err := s.DB.QueryRow(r.Context(), `SELECT count(*) FROM event_dead_letters WHERE site_id=$1 AND failed_at >= $2 AND failed_at < $3`, siteID, from, to).Scan(&deadLetters); err != nil {
		writeQueryError(w, err)
		return
	}
	// The cardinality limit is a setting rather than a measurement, and a site
	// environment that has none is a legitimate answer, so this one stays
	// tolerant: no row means no limit configured.
	var cardinalityLimit int64
	_ = s.DB.QueryRow(r.Context(), `SELECT cardinality_limit FROM site_environments WHERE site_id=$1 AND name=$2`, siteID, environment).Scan(&cardinalityLimit)
	cardinalityRows, cardinalityErr := s.DB.Query(r.Context(), `SELECT dimension,count(DISTINCT value_hash) FROM data_quality_dimension_values WHERE site_id=$1 AND environment=$2 AND event_date >= $3::date AND event_date < $4::date GROUP BY dimension ORDER BY count(DISTINCT value_hash) DESC`, siteID, environment, dateFrom, dateTo)
	// A query that failed is not a site with no crowded dimensions. Both halves
	// of this screen read as "nothing to worry about" when they are empty, which
	// is the one thing an empty answer must not be allowed to mean here.
	if cardinalityErr != nil {
		writeError(w, 500, "QUERY_FAILED", cardinalityErr.Error())
		return
	}
	cardinalities := []map[string]any{}
	if cardinalityRows != nil {
		defer cardinalityRows.Close()
		for cardinalityRows.Next() {
			var dimension string
			var count int64
			if err := cardinalityRows.Scan(&dimension, &count); err != nil {
				writeError(w, 500, "QUERY_FAILED", err.Error())
				return
			}
			level := "low"
			ratio := float64(0)
			if cardinalityLimit > 0 {
				ratio = float64(count) / float64(cardinalityLimit)
			}
			if ratio >= 1 {
				level = "extreme"
			} else if ratio >= .75 {
				level = "high"
			} else if ratio >= .25 {
				level = "medium"
			}
			cardinalities = append(cardinalities, map[string]any{"dimension": dimension, "distinct_values": count, "limit": cardinalityLimit, "level": level, "query_builder_allowed": level != "extreme"})
		}
		if err := cardinalityRows.Err(); err != nil {
			writeError(w, 500, "QUERY_FAILED", err.Error())
			return
		}
	}
	issueRows, issueErr := s.DB.Query(r.Context(), `SELECT code,severity,event_name,message,sample,occurred_at FROM data_quality_issues WHERE site_id=$1 AND environment=$2 AND occurred_at >= $3 AND occurred_at < $4 ORDER BY occurred_at DESC LIMIT 100`, siteID, environment, from, to)
	if issueErr != nil {
		writeError(w, 500, "QUERY_FAILED", issueErr.Error())
		return
	}
	issues := []map[string]any{}
	if issueRows != nil {
		defer issueRows.Close()
		for issueRows.Next() {
			var code, severity, eventName, message string
			var sample []byte
			var occurred time.Time
			if err := issueRows.Scan(&code, &severity, &eventName, &message, &sample, &occurred); err != nil {
				writeError(w, 500, "QUERY_FAILED", err.Error())
				return
			}
			var sampleValue any
			_ = json.Unmarshal(sample, &sampleValue)
			issues = append(issues, map[string]any{"code": code, "severity": severity, "event_name": eventName, "message": message, "sample": sampleValue, "occurred_at": occurred})
		}
		if err := issueRows.Err(); err != nil {
			writeError(w, 500, "QUERY_FAILED", err.Error())
			return
		}
	}
	writeJSON(w, 200, map[string]any{"health_score": score, "environment": environment, "collector": map[string]any{"received": received, "accepted": accepted, "pending": pending, "inbox_lag_seconds": inboxLag, "dead_letters": deadLetters}, "quality": map[string]any{"duplicates": duplicates, "warnings": warnings, "rejected": rejected, "late_events": late, "missing_user_id": missingUser, "refused_user_id": refusedUser, "missing_feature": missingFeature, "unknown_network": unknownNetwork, "pii_blocked": piiBlocked, "pii_detected": piiDetected, "cardinality_violations": cardinality}, "cardinalities": cardinalities, "issues": issues})
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
