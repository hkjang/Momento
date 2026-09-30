package httpapi

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

// The audit log answered the newest 500 entries and nothing else, so an entry
// older than that could not be found from the console at all. It now filters by
// action, actor and resource as literal text and pages backwards by id.
func TestAuditLogFiltersAndPagesBackwards(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()
	// The log outlives the fixture, so a previous run's entries are removed first.
	if _, err := pool.Exec(ctx, `DELETE FROM audit_logs WHERE action IN ('test.audit_page','test.auditXpage')`); err != nil {
		t.Fatalf("clear: %v", err)
	}

	// Twelve entries of an action nothing else writes, with an underscore in it
	// so a wildcard reading would also take the decoy.
	for i := range 12 {
		if _, err := pool.Exec(ctx, `INSERT INTO audit_logs(action,resource_type,resource_id,detail) VALUES('test.audit_page','site',$1,'{}')`, fmt.Sprintf("audit-res-%02d", i)); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO audit_logs(action,resource_type,resource_id,detail) VALUES('test.auditXpage','site','audit-decoy','{}')`); err != nil {
		t.Fatalf("insert decoy: %v", err)
	}

	list := func(t *testing.T, query string) []map[string]any {
		t.Helper()
		body := f.get(t, "/api/v1/audit?"+query)
		raw, _ := body["list"].([]any)
		out := make([]map[string]any, 0, len(raw))
		for _, item := range raw {
			entry, _ := item.(map[string]any)
			out = append(out, entry)
		}
		return out
	}

	t.Run("the action filter is literal text", func(t *testing.T) {
		entries := list(t, "action="+url.QueryEscape("audit_page"))
		if len(entries) != 12 {
			t.Fatalf("got %d entries, want the 12 test.audit_page entries", len(entries))
		}
		for _, entry := range entries {
			if entry["action"] != "test.audit_page" {
				t.Fatalf("the filter took %v", entry["action"])
			}
		}
	})

	t.Run("pages continue with before_id and never overlap", func(t *testing.T) {
		seen := map[string]bool{}
		cursor := ""
		var lastID float64
		for page := 0; page < 5; page++ {
			entries := list(t, "action=audit_page&limit=5"+cursor)
			for _, entry := range entries {
				id, _ := entry["id"].(float64)
				if lastID != 0 && id >= lastID {
					t.Fatalf("id %v is not older than %v", id, lastID)
				}
				lastID = id
				resource := fmt.Sprint(entry["resource_id"])
				if seen[resource] {
					t.Fatalf("%s appeared twice", resource)
				}
				seen[resource] = true
			}
			if len(entries) < 5 {
				break
			}
			cursor = fmt.Sprintf("&before_id=%.0f", lastID)
		}
		if len(seen) != 12 {
			t.Fatalf("paged through %d entries, want 12", len(seen))
		}
	})

	t.Run("resource and actor filters narrow the log", func(t *testing.T) {
		if entries := list(t, "resource=audit-res-07"); len(entries) != 1 {
			t.Fatalf("resource filter matched %d entries, want 1", len(entries))
		}
		// These were written with no actor, which the log names System.
		for _, entry := range list(t, "actor=system&action=audit_page") {
			if !strings.EqualFold(fmt.Sprint(entry["actor"]), "System") {
				t.Fatalf("actor filter took %v", entry["actor"])
			}
		}
	})

	t.Run("a malformed page request is refused", func(t *testing.T) {
		for _, query := range []string{"limit=0", "limit=5000", "limit=x", "before_id=-1", "before_id=abc"} {
			if code := f.rawGet(t, "/api/v1/audit?"+query, "").Code; code != 400 {
				t.Errorf("%s answered %d, want 400", query, code)
			}
		}
	})
}
