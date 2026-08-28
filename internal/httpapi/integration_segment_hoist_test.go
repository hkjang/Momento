package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/hkjang/Momento/internal/segment"
)

// A person-level aggregate compiles to an uncorrelated subquery, so writing it
// as a named CTE instead of inline is the same predicate. "The same predicate"
// is a claim about SQL, and the way to check a claim about SQL is to run both
// against the same data and compare who comes back.
//
// This matters because the cohort grid applies a segment twice — once to define
// the cohort and once to score the return activity — and the two inline copies
// are the same work done twice.
//
// The definitions below are chosen to be the shapes where a careless hoist would
// go wrong: an aggregate on its own, an aggregate OR'd with an event-level
// condition, an aggregate AND'd with one, two aggregates in one definition, and a
// nested group mixing all of it.
func TestHoistingASegmentAggregateSelectsTheSamePeople(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()

	resolver, err := segment.NewResolver(ctx, pool, f.siteID, "prd")
	if err != nil {
		t.Fatalf("resolver: %v", err)
	}

	for _, probe := range []struct{ name, definition string }{
		{"an aggregate on its own", `{"combinator":"and","rules":[{"field":"entity.sessions","operator":">=","value":1}]}`},
		{"an aggregate with an event-level condition", `{"combinator":"and","rules":[{"field":"entity.sessions","operator":">=","value":1},{"field":"device.type","operator":"=","value":"desktop"}]}`},
		{"an aggregate or an event-level condition", `{"combinator":"or","rules":[{"field":"entity.conversions","operator":">=","value":1},{"field":"device.type","operator":"=","value":"mobile"}]}`},
		// Two aggregates that both select somebody. A pair where one excludes the
		// other would compare two empty sets and agree however wrong either is.
		{"two aggregates", `{"combinator":"and","rules":[{"field":"entity.sessions","operator":">=","value":1},{"field":"entity.conversions","operator":">=","value":1}]}`},
		{"a nested group", `{"combinator":"and","rules":[{"field":"entity.sessions","operator":">=","value":1},{"combinator":"or","rules":[{"field":"entity.conversions","operator":">=","value":1},{"field":"browser","operator":"=","value":"Chrome"}]}]}`},
	} {
		t.Run(probe.name, func(t *testing.T) {
			var node segment.Node
			if err := json.Unmarshal([]byte(probe.definition), &node); err != nil {
				t.Fatalf("parse the definition: %v", err)
			}

			inlineArgs := []any{f.siteID, "prd"}
			inline, err := segment.Compile(node, resolver, "e", &inlineArgs, 0)
			if err != nil {
				t.Fatalf("compile inline: %v", err)
			}
			hoistArgs := []any{f.siteID, "prd"}
			hoisted, ctes, err := segment.CompileHoisted(node, resolver, "e", &hoistArgs, 0, "segment_probe_")
			if err != nil {
				t.Fatalf("compile hoisted: %v", err)
			}
			if len(ctes) == 0 {
				t.Fatalf("nothing was lifted out of %q, so the two forms are the same text and this compares nothing", probe.definition)
			}
			if strings.Contains(hoisted, "GROUP BY segment_entity.entity_id") {
				t.Errorf("the hoisted predicate still writes an aggregate inline: %s", hoisted)
			}

			read := func(predicate string, with []string, args []any) []string {
				t.Helper()
				query := ""
				if len(with) > 0 {
					query = "WITH " + strings.Join(with, ",\n") + "\n"
				}
				query += `SELECT DISTINCT e.entity_id FROM analytics_events e
					WHERE e.site_id=$1 AND e.environment=$2 AND (` + predicate + `) ORDER BY 1`
				rows, err := pool.Query(ctx, query, args...)
				if err != nil {
					t.Fatalf("run %s: %v", predicate, err)
				}
				defer rows.Close()
				found := []string{}
				for rows.Next() {
					var entity string
					if rows.Scan(&entity) == nil {
						found = append(found, entity)
					}
				}
				if err := rows.Err(); err != nil {
					t.Fatalf("read: %v", err)
				}
				return found
			}

			before := read(inline, nil, inlineArgs)
			after := read(hoisted, ctes, hoistArgs)
			// Both sides must select somebody, or two empty sets would agree
			// however wrong either compilation is.
			if len(before) == 0 {
				t.Fatalf("the inline form selects nobody, so agreement proves nothing")
			}
			if fmt.Sprint(before) != fmt.Sprint(after) {
				t.Errorf("the two forms select different people:\ninline  %v\nhoisted %v", before, after)
			}
		})
	}
}
