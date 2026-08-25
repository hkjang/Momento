package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/hkjang/Momento/internal/auth"
)

type segmentNode struct {
	Combinator string        `json:"combinator,omitempty"`
	Rules      []segmentNode `json:"rules,omitempty"`
	Field      string        `json:"field,omitempty"`
	Operator   string        `json:"operator,omitempty"`
	Value      any           `json:"value,omitempty"`
}

type customDimension struct {
	Name        string
	PropertyKey string
	Scope       string
	DataType    string
}

type dimensionResolver struct {
	custom map[string]customDimension
}

var propertyKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,127}$`)

var builtinDimensionSQL = map[string]string{
	"event.name":        "%s.event_name",
	"page.url":          "%s.page_url",
	"device.type":       "%s.device_type",
	"browser":           "%s.browser",
	"os":                "%s.os",
	"country":           "%s.country",
	"traffic.source":    "%s.source",
	"traffic.medium":    "%s.medium",
	"traffic.campaign":  "%s.campaign",
	"traffic.class":     "%s.traffic_class",
	"network":           "%s.network_name",
	"user.id":           "%s.canonical_user_id",
	"visitor.id":        "%s.visitor_id",
	"session.id":        "%s.session_id",
	"user.department":   "%s.canonical_user_properties->>'department'",
	"user.organization": "%s.canonical_user_properties->>'organization'",
	"feature":           "%s.properties->>'feature'",
	"button":            "coalesce(%s.properties->>'button',%s.properties->>'element_text')",
	"is_conversion":     "%s.is_conversion::text",
	"environment":       "%s.environment",
	"contract.version":  "%s.contract_version::text",
	"release.version":   "%s.properties->>'release_version'",
	"app.version":       "%s.properties->>'app_version'",
}

func (s *Server) newDimensionResolver(ctx context.Context, siteID uuid.UUID) (dimensionResolver, error) {
	rows, err := s.DB.Query(ctx, `SELECT name,property_key,scope,data_type FROM dimensions WHERE site_id=$1 AND active`, siteID)
	if err != nil {
		return dimensionResolver{}, err
	}
	defer rows.Close()
	resolver := dimensionResolver{custom: map[string]customDimension{}}
	for rows.Next() {
		var item customDimension
		if err := rows.Scan(&item.Name, &item.PropertyKey, &item.Scope, &item.DataType); err != nil {
			return dimensionResolver{}, err
		}
		resolver.custom["custom."+item.Name] = item
	}
	return resolver, rows.Err()
}

func (r dimensionResolver) expression(field, alias string) (string, error) {
	if template, ok := builtinDimensionSQL[field]; ok {
		if strings.Count(template, "%s") == 2 {
			return fmt.Sprintf(template, alias, alias), nil
		}
		return fmt.Sprintf(template, alias), nil
	}
	item, ok := r.custom[field]
	if !ok {
		return "", fmt.Errorf("unsupported dimension: %s", field)
	}
	if !propertyKeyPattern.MatchString(item.PropertyKey) {
		return "", fmt.Errorf("custom dimension has an invalid property key")
	}
	switch item.Scope {
	case "user":
		return fmt.Sprintf("%s.canonical_user_properties->>'%s'", alias, item.PropertyKey), nil
	case "event", "session":
		return fmt.Sprintf("%s.properties->>'%s'", alias, item.PropertyKey), nil
	default:
		return "", fmt.Errorf("item-scoped dimensions are available in Ecommerce reports")
	}
}

func compileSegment(node segmentNode, resolver dimensionResolver, alias string, args *[]any, depth int) (string, error) {
	if depth > 5 {
		return "", fmt.Errorf("segment nesting is limited to 5 levels")
	}
	if len(node.Rules) > 0 || node.Combinator != "" {
		combinator := strings.ToLower(node.Combinator)
		if combinator != "and" && combinator != "or" {
			return "", fmt.Errorf("segment combinator must be and or or")
		}
		if len(node.Rules) == 0 {
			return "", fmt.Errorf("segment group must contain at least one rule")
		}
		if len(node.Rules) > 50 {
			return "", fmt.Errorf("segment group is limited to 50 rules")
		}
		parts := make([]string, 0, len(node.Rules))
		for _, rule := range node.Rules {
			part, err := compileSegment(rule, resolver, alias, args, depth+1)
			if err != nil {
				return "", err
			}
			parts = append(parts, part)
		}
		return "(" + strings.Join(parts, " "+strings.ToUpper(combinator)+" ") + ")", nil
	}
	if node.Field == "event.has" {
		return compileEventExistence(node, alias, args)
	}
	if expression, ok := entityAggregateSQL[node.Field]; ok {
		return compileEntityAggregate(node, expression, alias, args)
	}
	expr, err := resolver.expression(node.Field, alias)
	if err != nil {
		return "", err
	}
	op := node.Operator
	switch op {
	case "exists":
		return "(" + expr + " IS NOT NULL AND " + expr + "<>'')", nil
	case "not exists":
		return "(" + expr + " IS NULL OR " + expr + "='')", nil
	case "in", "not in":
		raw, ok := node.Value.([]any)
		if !ok || len(raw) == 0 || len(raw) > 100 {
			return "", fmt.Errorf("%s requires an array with 1 to 100 values", op)
		}
		values := make([]string, 0, len(raw))
		for _, current := range raw {
			values = append(values, fmt.Sprint(current))
		}
		*args = append(*args, values)
		placeholder := "$" + strconv.Itoa(len(*args))
		if op == "in" {
			return expr + " = ANY(" + placeholder + ")", nil
		}
		return expr + " <> ALL(" + placeholder + ")", nil
	case "=", "!=", "contains", "not contains", "startsWith", "endsWith":
		*args = append(*args, fmt.Sprint(node.Value))
		placeholder := "$" + strconv.Itoa(len(*args))
		switch op {
		case "=", "!=":
			return expr + " " + op + " " + placeholder, nil
		case "contains":
			return expr + " ILIKE '%'||" + placeholder + "||'%'", nil
		case "not contains":
			return expr + " NOT ILIKE '%'||" + placeholder + "||'%'", nil
		case "startsWith":
			return expr + " ILIKE " + placeholder + "||'%'", nil
		default:
			return expr + " ILIKE '%'||" + placeholder, nil
		}
	case ">", ">=", "<", "<=":
		value, ok := numericValue(node.Value)
		if !ok {
			return "", fmt.Errorf("%s requires a numeric value", op)
		}
		*args = append(*args, value)
		placeholder := "$" + strconv.Itoa(len(*args))
		return "(CASE WHEN " + expr + " ~ '^-?[0-9]+(\\.[0-9]+)?$' THEN (" + expr + ")::numeric END) " + op + " " + placeholder, nil
	default:
		return "", fmt.Errorf("unsupported operator: %s", op)
	}
}

// entityAggregateSQL holds behavioural fields measured over a person's whole
// history rather than a single event. They make audiences such as "visited three
// times but never converted" or "dormant for 30 days" expressible as a segment.
var entityAggregateSQL = map[string]string{
	"entity.sessions":              "count(DISTINCT segment_entity.session_id)",
	"entity.events":                "count(*)",
	"entity.conversions":           "count(*) FILTER(WHERE segment_entity.is_conversion)",
	"entity.days_since_last_seen":  "extract(epoch FROM (now()-max(segment_entity.event_timestamp)))/86400",
	"entity.days_since_first_seen": "extract(epoch FROM (now()-min(segment_entity.event_timestamp)))/86400",
	// Friction and search fields. The tracker reports these signals on its own,
	// so "hit friction and never converted" or "searched and found nothing" are
	// audiences a site can define without instrumenting anything. Naming the
	// signals here rather than accepting an event name keeps the aggregate a
	// fixed expression, which is what makes it safe to inline.
	"entity.frustration_signals":  "count(*) FILTER(WHERE segment_entity.event_name = ANY('{rage_click,dead_click,rapid_back,form_retry,repeated_search,error_after_click,slow_interaction,error,resource_error}'))",
	"entity.frustration_sessions": "count(DISTINCT segment_entity.session_id) FILTER(WHERE segment_entity.event_name = ANY('{rage_click,dead_click,rapid_back,form_retry,repeated_search,error_after_click,slow_interaction,error,resource_error}'))",
	"entity.searches":             "count(*) FILTER(WHERE segment_entity.event_name='search')",
	"entity.zero_result_searches": "count(*) FILTER(WHERE segment_entity.event_name='search_no_result' OR (segment_entity.event_name='search' AND segment_entity.properties->>'result_count'='0'))",
	"entity.search_clicks":        "count(*) FILTER(WHERE segment_entity.event_name='search_click')",
}

// compileEntityAggregate compares one behavioural aggregate of the same person.
func compileEntityAggregate(node segmentNode, expression, alias string, args *[]any) (string, error) {
	switch node.Operator {
	case "=", "!=", ">", ">=", "<", "<=":
	default:
		return "", fmt.Errorf("%s requires a numeric comparison operator", node.Field)
	}
	value, ok := numericValue(node.Value)
	if !ok {
		return "", fmt.Errorf("%s requires a numeric value", node.Field)
	}
	*args = append(*args, value)
	placeholder := "$" + strconv.Itoa(len(*args))
	subquery := "(SELECT " + expression + " FROM analytics_events segment_entity WHERE segment_entity.site_id=" + alias +
		".site_id AND segment_entity.environment=" + alias + ".environment AND segment_entity.entity_id=" + alias + ".entity_id)"
	operator := node.Operator
	if operator == "!=" {
		operator = "<>"
	}
	return "coalesce(" + subquery + ",0) " + operator + " " + placeholder, nil
}

func compileEventExistence(node segmentNode, alias string, args *[]any) (string, error) {
	identity := "segment_event.entity_id=" + alias + ".entity_id"
	base := "segment_event.site_id=" + alias + ".site_id AND segment_event.environment=" + alias + ".environment AND " + identity
	switch node.Operator {
	case "=", "!=":
		name := strings.TrimSpace(fmt.Sprint(node.Value))
		if !eventNamePatternForProperty.MatchString(name) {
			return "", fmt.Errorf("event.has requires a valid event name")
		}
		*args = append(*args, name)
		exists := "EXISTS(SELECT 1 FROM analytics_events segment_event WHERE " + base + " AND segment_event.event_name=$" + strconv.Itoa(len(*args)) + ")"
		if node.Operator == "!=" {
			return "NOT " + exists, nil
		}
		return exists, nil
	case "in", "not in":
		raw, ok := node.Value.([]any)
		if !ok || len(raw) == 0 || len(raw) > 100 {
			return "", fmt.Errorf("event.has %s requires 1 to 100 event names", node.Operator)
		}
		values := make([]string, 0, len(raw))
		for _, value := range raw {
			name := strings.TrimSpace(fmt.Sprint(value))
			if !eventNamePatternForProperty.MatchString(name) {
				return "", fmt.Errorf("event.has requires valid event names")
			}
			values = append(values, name)
		}
		*args = append(*args, values)
		exists := "EXISTS(SELECT 1 FROM analytics_events segment_event WHERE " + base + " AND segment_event.event_name=ANY($" + strconv.Itoa(len(*args)) + "))"
		if node.Operator == "not in" {
			return "NOT " + exists, nil
		}
		return exists, nil
	default:
		return "", fmt.Errorf("event.has supports =, !=, in, and not in")
	}
}

func numericValue(value any) (float64, bool) {
	switch current := value.(type) {
	case float64:
		return current, true
	case float32:
		return float64(current), true
	case int:
		return float64(current), true
	case int64:
		return float64(current), true
	case json.Number:
		parsed, err := current.Float64()
		return parsed, err == nil
	default:
		parsed, err := strconv.ParseFloat(fmt.Sprint(value), 64)
		return parsed, err == nil
	}
}

func (s *Server) loadSegment(ctx context.Context, siteID uuid.UUID, id string) (segmentNode, error) {
	segmentID, err := uuid.Parse(id)
	if err != nil {
		return segmentNode{}, fmt.Errorf("invalid segment id")
	}
	p, _ := auth.FromContext(ctx)
	var raw []byte
	err = s.DB.QueryRow(ctx, `SELECT definition FROM segments WHERE id=$1 AND site_id=$2 AND (shared OR owner_id=$3 OR $4 IN ('super_admin','organization_admin','workspace_admin'))`, segmentID, siteID, p.ID, p.Role).Scan(&raw)
	if err != nil {
		return segmentNode{}, fmt.Errorf("segment not found")
	}
	var definition segmentNode
	if err := json.Unmarshal(raw, &definition); err != nil {
		return segmentNode{}, err
	}
	return definition, nil
}

func (s *Server) listSegments(w http.ResponseWriter, r *http.Request) {
	siteKey := r.URL.Query().Get("site_id")
	siteID, err := s.resolveSiteKey(r.Context(), siteKey)
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	p, _ := auth.FromContext(r.Context())
	rows, err := s.DB.Query(r.Context(), `SELECT g.id,g.name,g.description,g.definition,g.shared,g.owner_id,u.display_name,g.created_at,g.updated_at FROM segments g LEFT JOIN users u ON u.id=g.owner_id WHERE g.site_id=$1 AND (g.shared OR g.owner_id=$2 OR $3 IN ('super_admin','organization_admin','workspace_admin')) ORDER BY g.updated_at DESC`, siteID, p.ID, p.Role)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var name, description string
		var raw []byte
		var shared bool
		var ownerID *uuid.UUID
		var owner *string
		var created, updated time.Time
		if rows.Scan(&id, &name, &description, &raw, &shared, &ownerID, &owner, &created, &updated) == nil {
			var definition any
			_ = json.Unmarshal(raw, &definition)
			out = append(out, map[string]any{"id": id, "name": name, "description": description, "definition": definition, "shared": shared, "owner_id": ownerID, "owner": owner, "created_at": created, "updated_at": updated})
		}
	}
	writeJSON(w, 200, out)
}

type segmentSaveRequest struct {
	SiteID      string      `json:"site_id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Shared      bool        `json:"shared"`
	Definition  segmentNode `json:"definition"`
}

func (s *Server) createSegment(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	var in segmentSaveRequest
	if err := decodeJSON(r, &in, 256<<10); err != nil {
		writeError(w, 400, "INVALID_PAYLOAD", err.Error())
		return
	}
	siteID, err := s.resolveSiteKey(r.Context(), in.SiteID)
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	if strings.TrimSpace(in.Name) == "" {
		writeError(w, 400, "INVALID_NAME", "segment name is required")
		return
	}
	resolver, err := s.newDimensionResolver(r.Context(), siteID)
	args := []any{}
	if err == nil {
		_, err = compileSegment(in.Definition, resolver, "e", &args, 0)
	}
	if err != nil {
		writeError(w, 400, "INVALID_SEGMENT", err.Error())
		return
	}
	if in.Shared && !auth.RoleAtLeast(p.Role, "workspace_admin") {
		writeError(w, 403, "FORBIDDEN", "administrator permission is required for shared segments")
		return
	}
	body, _ := json.Marshal(in.Definition)
	var id uuid.UUID
	err = s.DB.QueryRow(r.Context(), `INSERT INTO segments(site_id,owner_id,name,description,definition,shared) VALUES($1,$2,$3,$4,$5,$6) RETURNING id`, siteID, p.ID, strings.TrimSpace(in.Name), in.Description, body, in.Shared).Scan(&id)
	if err != nil {
		writeError(w, 409, "SEGMENT_SAVE_FAILED", err.Error())
		return
	}
	s.audit(r.Context(), &p, "segment.create", "segment", id.String(), map[string]any{"name": in.Name}, clientIP(r))
	writeJSON(w, 201, map[string]any{"id": id})
}

func (s *Server) updateSegment(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "INVALID_ID", "invalid segment id")
		return
	}
	var in segmentSaveRequest
	if err := decodeJSON(r, &in, 256<<10); err != nil {
		writeError(w, 400, "INVALID_PAYLOAD", err.Error())
		return
	}
	siteID, err := s.resolveSiteKey(r.Context(), in.SiteID)
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	resolver, err := s.newDimensionResolver(r.Context(), siteID)
	args := []any{}
	if err == nil {
		_, err = compileSegment(in.Definition, resolver, "e", &args, 0)
	}
	if err != nil || strings.TrimSpace(in.Name) == "" {
		writeError(w, 400, "INVALID_SEGMENT", "segment name and a valid definition are required")
		return
	}
	if in.Shared && !auth.RoleAtLeast(p.Role, "workspace_admin") {
		writeError(w, 403, "FORBIDDEN", "administrator permission is required for shared segments")
		return
	}
	body, _ := json.Marshal(in.Definition)
	tag, err := s.DB.Exec(r.Context(), `UPDATE segments SET name=$2,description=$3,definition=$4,shared=$5,updated_at=now() WHERE id=$1 AND site_id=$6 AND (owner_id=$7 OR $8 IN ('super_admin','organization_admin','workspace_admin'))`, id, strings.TrimSpace(in.Name), in.Description, body, in.Shared, siteID, p.ID, p.Role)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, 404, "NOT_FOUND", "segment not found or not editable")
		return
	}
	s.audit(r.Context(), &p, "segment.update", "segment", id.String(), nil, clientIP(r))
	writeJSON(w, 200, map[string]bool{"updated": true})
}

func (s *Server) deleteSegment(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "INVALID_ID", "invalid segment id")
		return
	}
	tag, err := s.DB.Exec(r.Context(), `DELETE FROM segments g USING sites s WHERE g.id=$1 AND g.site_id=s.id AND (g.owner_id=$2 OR $3 IN ('super_admin','organization_admin') OR EXISTS(SELECT 1 FROM user_workspace_roles uwr WHERE uwr.workspace_id=s.workspace_id AND uwr.user_id=$2 AND uwr.role='workspace_admin'))`, id, p.ID, p.Role)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, 404, "NOT_FOUND", "segment not found or not editable")
		return
	}
	s.audit(r.Context(), &p, "segment.delete", "segment", id.String(), nil, clientIP(r))
	w.WriteHeader(204)
}

func (s *Server) listDimensions(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSiteKey(r.Context(), r.URL.Query().Get("site_id"))
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	rows, err := s.DB.Query(r.Context(), `SELECT id,name,property_key,scope,data_type,description,active,updated_at FROM dimensions WHERE site_id=$1 ORDER BY name`, siteID)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var name, propertyKey, scope, dataType, description string
		var active bool
		var updated time.Time
		if rows.Scan(&id, &name, &propertyKey, &scope, &dataType, &description, &active, &updated) == nil {
			out = append(out, map[string]any{"id": id, "name": name, "query_name": "custom." + name, "property_key": propertyKey, "scope": scope, "data_type": dataType, "description": description, "active": active, "updated_at": updated})
		}
	}
	writeJSON(w, 200, out)
}

func (s *Server) saveDimension(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	var in struct {
		SiteID      string `json:"site_id"`
		Name        string `json:"name"`
		PropertyKey string `json:"property_key"`
		Scope       string `json:"scope"`
		DataType    string `json:"data_type"`
		Description string `json:"description"`
		Active      *bool  `json:"active"`
	}
	if err := decodeJSON(r, &in, 64<<10); err != nil {
		writeError(w, 400, "INVALID_PAYLOAD", err.Error())
		return
	}
	siteID, err := s.resolveSiteKey(r.Context(), in.SiteID)
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	in.Name = strings.ToLower(strings.TrimSpace(in.Name))
	if !propertyKeyPattern.MatchString(in.Name) || !propertyKeyPattern.MatchString(in.PropertyKey) {
		writeError(w, 400, "INVALID_DIMENSION", "name and property_key must use letters, numbers, underscore, dot, or hyphen")
		return
	}
	if in.Scope != "user" && in.Scope != "session" && in.Scope != "event" && in.Scope != "item" {
		writeError(w, 400, "INVALID_SCOPE", "scope must be user, session, event, or item")
		return
	}
	if in.DataType == "" {
		in.DataType = "string"
	}
	if in.DataType != "string" && in.DataType != "number" && in.DataType != "boolean" && in.DataType != "date" {
		writeError(w, 400, "INVALID_DATA_TYPE", "data_type must be string, number, boolean, or date")
		return
	}
	active := true
	if in.Active != nil {
		active = *in.Active
	}
	var id uuid.UUID
	err = s.DB.QueryRow(r.Context(), `INSERT INTO dimensions(site_id,name,property_key,scope,data_type,description,active,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,now()) ON CONFLICT(site_id,name) DO UPDATE SET property_key=excluded.property_key,scope=excluded.scope,data_type=excluded.data_type,description=excluded.description,active=excluded.active,updated_at=now() RETURNING id`, siteID, in.Name, in.PropertyKey, in.Scope, in.DataType, in.Description, active).Scan(&id)
	if err != nil {
		writeError(w, 500, "DIMENSION_SAVE_FAILED", err.Error())
		return
	}
	s.audit(r.Context(), &p, "dimension.save", "dimension", id.String(), map[string]any{"name": in.Name, "scope": in.Scope}, clientIP(r))
	writeJSON(w, 200, map[string]any{"id": id, "query_name": "custom." + in.Name})
}

func (s *Server) deleteDimension(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "INVALID_ID", "invalid dimension id")
		return
	}
	tag, err := s.DB.Exec(r.Context(), `DELETE FROM dimensions d USING sites s WHERE d.id=$1 AND d.site_id=s.id AND ($2 IN ('super_admin','organization_admin') OR EXISTS(SELECT 1 FROM user_workspace_roles uwr WHERE uwr.workspace_id=s.workspace_id AND uwr.user_id=$3))`, id, p.Role, p.ID)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, 404, "NOT_FOUND", "dimension not found")
		return
	}
	s.audit(r.Context(), &p, "dimension.delete", "dimension", id.String(), nil, clientIP(r))
	w.WriteHeader(204)
}

func (s *Server) listSavedReports(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSiteKey(r.Context(), r.URL.Query().Get("site_id"))
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	p, _ := auth.FromContext(r.Context())
	kind := r.URL.Query().Get("kind")
	rows, err := s.DB.Query(r.Context(), `SELECT q.id,q.kind,q.name,q.description,q.definition,q.shared,q.owner_id,u.display_name,q.created_at,q.updated_at FROM saved_reports q LEFT JOIN users u ON u.id=q.owner_id WHERE q.site_id=$1 AND ($2='' OR q.kind=$2) AND (q.shared OR q.owner_id=$3 OR $4 IN ('super_admin','organization_admin','workspace_admin')) ORDER BY q.updated_at DESC`, siteID, kind, p.ID, p.Role)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var reportKind, name, description string
		var definition []byte
		var shared bool
		var ownerID *uuid.UUID
		var owner *string
		var created, updated time.Time
		if rows.Scan(&id, &reportKind, &name, &description, &definition, &shared, &ownerID, &owner, &created, &updated) == nil {
			var value any
			_ = json.Unmarshal(definition, &value)
			out = append(out, map[string]any{"id": id, "kind": reportKind, "name": name, "description": description, "definition": value, "shared": shared, "owner_id": ownerID, "owner": owner, "created_at": created, "updated_at": updated})
		}
	}
	writeJSON(w, 200, out)
}

type savedReportRequest struct {
	SiteID      string         `json:"site_id"`
	Kind        string         `json:"kind"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Definition  map[string]any `json:"definition"`
	Shared      bool           `json:"shared"`
}

func validateSavedReport(in savedReportRequest, p auth.Principal) error {
	validKind := in.Kind == "exploration" || in.Kind == "funnel" || in.Kind == "path" || in.Kind == "dashboard"
	if !validKind || strings.TrimSpace(in.Name) == "" || in.Definition == nil {
		return fmt.Errorf("kind, name, and definition are required")
	}
	if in.Shared && !auth.RoleAtLeast(p.Role, "workspace_admin") {
		return fmt.Errorf("administrator permission is required for shared reports")
	}
	return nil
}

func (s *Server) createSavedReport(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	var in savedReportRequest
	if err := decodeJSON(r, &in, 256<<10); err != nil {
		writeError(w, 400, "INVALID_PAYLOAD", err.Error())
		return
	}
	if err := validateSavedReport(in, p); err != nil {
		writeError(w, 400, "INVALID_REPORT", err.Error())
		return
	}
	siteID, err := s.resolveSiteKey(r.Context(), in.SiteID)
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	body, _ := json.Marshal(in.Definition)
	var id uuid.UUID
	err = s.DB.QueryRow(r.Context(), `INSERT INTO saved_reports(site_id,owner_id,kind,name,description,definition,shared) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id`, siteID, p.ID, in.Kind, strings.TrimSpace(in.Name), in.Description, body, in.Shared).Scan(&id)
	if err != nil {
		writeError(w, 500, "REPORT_SAVE_FAILED", err.Error())
		return
	}
	s.audit(r.Context(), &p, "report.create", "saved_report", id.String(), map[string]any{"name": in.Name, "kind": in.Kind}, clientIP(r))
	writeJSON(w, 201, map[string]any{"id": id})
}

func (s *Server) updateSavedReport(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "INVALID_ID", "invalid report id")
		return
	}
	var in savedReportRequest
	if err := decodeJSON(r, &in, 256<<10); err != nil {
		writeError(w, 400, "INVALID_PAYLOAD", err.Error())
		return
	}
	if err := validateSavedReport(in, p); err != nil {
		writeError(w, 400, "INVALID_REPORT", err.Error())
		return
	}
	siteID, err := s.resolveSiteKey(r.Context(), in.SiteID)
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	body, _ := json.Marshal(in.Definition)
	tag, err := s.DB.Exec(r.Context(), `UPDATE saved_reports SET kind=$2,name=$3,description=$4,definition=$5,shared=$6,updated_at=now() WHERE id=$1 AND site_id=$7 AND (owner_id=$8 OR $9 IN ('super_admin','organization_admin','workspace_admin'))`, id, in.Kind, strings.TrimSpace(in.Name), in.Description, body, in.Shared, siteID, p.ID, p.Role)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, 404, "NOT_FOUND", "report not found or not editable")
		return
	}
	s.audit(r.Context(), &p, "report.update", "saved_report", id.String(), nil, clientIP(r))
	writeJSON(w, 200, map[string]bool{"updated": true})
}

func (s *Server) deleteSavedReport(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "INVALID_ID", "invalid report id")
		return
	}
	tag, err := s.DB.Exec(r.Context(), `DELETE FROM saved_reports q USING sites s WHERE q.id=$1 AND q.site_id=s.id AND (q.owner_id=$2 OR $3 IN ('super_admin','organization_admin') OR EXISTS(SELECT 1 FROM user_workspace_roles uwr WHERE uwr.workspace_id=s.workspace_id AND uwr.user_id=$2 AND uwr.role='workspace_admin'))`, id, p.ID, p.Role)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, 404, "NOT_FOUND", "report not found or not editable")
		return
	}
	s.audit(r.Context(), &p, "report.delete", "saved_report", id.String(), nil, clientIP(r))
	w.WriteHeader(204)
}
