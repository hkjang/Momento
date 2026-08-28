// Package segment compiles a saved segment definition into SQL.
//
// It lives outside the HTTP layer because a segment is not a screen. The console
// evaluates one, the MCP surface evaluates one, and a scheduled delivery has to
// evaluate the same one — and while this code sat in the HTTP package, the
// scheduled delivery could not reach it. What it sent instead was a filter over
// three properties that happened to be called "Segment 집계", so the same word
// meant two different populations depending on which door it came through.
package segment

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Node struct {
	Combinator string `json:"combinator,omitempty"`
	Rules      []Node `json:"rules,omitempty"`
	Field      string `json:"field,omitempty"`
	Operator   string `json:"operator,omitempty"`
	Value      any    `json:"value,omitempty"`
}

// CustomDimension is a dimension an administrator defined for the site, mapped
// onto the property the collector stores it in.
type CustomDimension struct {
	Name        string
	PropertyKey string
	Scope       string
	DataType    string
}

type Resolver struct {
	custom map[string]CustomDimension
	// siteID and environment scope the behavioural aggregates. They are held here
	// rather than read from the outer row so the aggregate subquery is a constant
	// the planner evaluates once; correlating it made every candidate row run its
	// own aggregate, which no site of real size could survive.
	siteID      uuid.UUID
	environment string
}

// eventNamePattern and propertyKeyPattern accept the same shape: a name the
// collector would have stored. They are separate constants because they answer
// different questions and one may narrow without the other.
var eventNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,127}$`)

// PropertyKeyPattern is the shape a custom dimension's property key must have.
var PropertyKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,127}$`)

var builtinDimensionSQL = map[string]string{
	"event.name":       "%s.event_name",
	"page.url":         "%s.page_url",
	"device.type":      "%s.device_type",
	"browser":          "%s.browser",
	"os":               "%s.os",
	"country":          "%s.country",
	"traffic.source":   "%s.source",
	"traffic.medium":   "%s.medium",
	"traffic.campaign": "%s.campaign",
	"traffic.class":    "%s.traffic_class",
	"network":          "%s.network_name",
	// Whether the event came from a network an administrator marked internal. The
	// collector has always recorded this and nothing could read it, so "exclude
	// our own staff" was expressible only by naming every internal network.
	"traffic.internal":  "%s.is_internal",
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

// ResolverFor builds a resolver from dimensions already in hand. Reading them
// is a database round trip, so a caller that has them — or a test that is
// stating them — does not have to make one.
func ResolverFor(siteID uuid.UUID, environment string, custom map[string]CustomDimension) Resolver {
	if custom == nil {
		custom = map[string]CustomDimension{}
	}
	return Resolver{custom: custom, siteID: siteID, environment: environment}
}

// NewResolver reads the site's custom dimensions once so a compiled segment can
// name them.
func NewResolver(ctx context.Context, db *pgxpool.Pool, siteID uuid.UUID, environment string) (Resolver, error) {
	rows, err := db.Query(ctx, `SELECT name,property_key,scope,data_type FROM dimensions WHERE site_id=$1 AND active`, siteID)
	if err != nil {
		return Resolver{}, err
	}
	defer rows.Close()
	resolver := Resolver{custom: map[string]CustomDimension{}, siteID: siteID, environment: environment}
	for rows.Next() {
		var item CustomDimension
		if err := rows.Scan(&item.Name, &item.PropertyKey, &item.Scope, &item.DataType); err != nil {
			return Resolver{}, err
		}
		resolver.custom["custom."+item.Name] = item
	}
	return resolver, rows.Err()
}

// Expression is the SQL for one dimension, built in or custom.
func (r Resolver) Expression(field, alias string) (string, error) {
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
	if !PropertyKeyPattern.MatchString(item.PropertyKey) {
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

func Compile(node Node, resolver Resolver, alias string, args *[]any, depth int) (string, error) {
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
			part, err := Compile(rule, resolver, alias, args, depth+1)
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
		return compileEntityAggregate(node, resolver, expression, alias, args)
	}
	expr, err := resolver.Expression(node.Field, alias)
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
//
// It compiles to a semi-join against a grouped subquery rather than to a
// correlated aggregate. The two forms select the same people, but the correlated
// form re-ran the aggregate for every candidate row: on two million events it
// did not finish inside a minute, which is past the analytical deadline, so
// behavioural segments were unusable on any site of real size. Grouping once and
// hashing the result is a single pass.
//
// The subquery has no time bound because these fields are defined over a
// person's whole history; the site and environment come from the resolver so the
// subquery stays constant.
func compileEntityAggregate(node Node, resolver Resolver, expression, alias string, args *[]any) (string, error) {
	switch node.Operator {
	case "=", "!=", ">", ">=", "<", "<=":
	default:
		return "", fmt.Errorf("%s requires a numeric comparison operator", node.Field)
	}
	value, ok := numericValue(node.Value)
	if !ok {
		return "", fmt.Errorf("%s requires a numeric value", node.Field)
	}
	if resolver.siteID == uuid.Nil || resolver.environment == "" {
		// Without a scope the aggregate would silently measure the wrong
		// population, so it fails instead of guessing.
		return "", fmt.Errorf("%s cannot be evaluated without a site and environment", node.Field)
	}
	*args = append(*args, resolver.siteID, resolver.environment, value)
	sitePlaceholder := "$" + strconv.Itoa(len(*args)-2)
	environmentPlaceholder := "$" + strconv.Itoa(len(*args)-1)
	valuePlaceholder := "$" + strconv.Itoa(len(*args))
	operator := node.Operator
	if operator == "!=" {
		operator = "<>"
	}
	return alias + ".entity_id IN (SELECT segment_entity.entity_id FROM analytics_events segment_entity" +
		" WHERE segment_entity.site_id=" + sitePlaceholder + " AND segment_entity.environment=" + environmentPlaceholder +
		" GROUP BY segment_entity.entity_id HAVING coalesce(" + expression + ",0) " + operator + " " + valuePlaceholder + ")", nil
}

func compileEventExistence(node Node, alias string, args *[]any) (string, error) {
	identity := "segment_event.entity_id=" + alias + ".entity_id"
	base := "segment_event.site_id=" + alias + ".site_id AND segment_event.environment=" + alias + ".environment AND " + identity
	switch node.Operator {
	case "=", "!=":
		name := strings.TrimSpace(fmt.Sprint(node.Value))
		if !eventNamePattern.MatchString(name) {
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
			if !eventNamePattern.MatchString(name) {
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
