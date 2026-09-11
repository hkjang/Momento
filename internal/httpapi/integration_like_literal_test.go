package httpapi

import (
	"context"
	"net/url"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hkjang/Momento/internal/segment"
)

// "contains" and the visitor search take text, and the text has to select the
// pages that contain it — not the pages that contain a pattern it happens to
// spell. The two shapes that go wrong are an underscore, which LIKE reads as any
// one character, and a percent sign, which it reads as any run of them and which
// every URL-encoded Korean path is full of.
func TestTextMatchingSelectsTheTextAndNotAPattern(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()
	from, today := f.siteDates(t, 3)
	site := "/api/v1/sites/" + f.siteKey

	base := time.Now().Add(-2 * time.Hour)
	insert := func(visitor, page string, offset int) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO raw_events(event_id,site_id,environment,visitor_id,session_id,event_name,event_timestamp,received_at,properties,is_conversion,page_url)
			VALUES($1,$2,'prd',$3,$4,'page_view',$5,$5,'{}'::jsonb,false,$6)`,
			uuid.NewString(), f.siteID, visitor, visitor+"-s", base.Add(time.Duration(offset)*time.Second), page); err != nil {
			t.Fatalf("insert %s for %s: %v", page, visitor, err)
		}
	}
	// One person on the page the text names, one on a page that only a wildcard
	// reading of the text would take.
	insert("like-underscore-hit", "https://portal.internal/report_daily", 1)
	insert("like-underscore-miss", "https://portal.internal/reportXdaily", 2)
	insert("like-percent-hit", "https://portal.internal/files/%EA%B0%80", 3)
	insert("like-percent-miss", "https://portal.internal/files/EA-B0-80", 4)

	resolver := segment.ResolverFor(f.siteID, "prd", nil)
	people := func(t *testing.T, operator, value string) []string {
		t.Helper()
		args := []any{f.siteID, "prd"}
		predicate, err := segment.Compile(segment.Node{Field: "page.url", Operator: operator, Value: value}, resolver, "e", &args, 0)
		if err != nil {
			t.Fatalf("compile %s %q: %v", operator, value, err)
		}
		rows, err := pool.Query(ctx, `SELECT DISTINCT e.visitor_id FROM analytics_events e
			WHERE e.site_id=$1 AND e.environment=$2 AND e.visitor_id LIKE 'like-%' AND (`+predicate+`) ORDER BY 1`, args...)
		if err != nil {
			t.Fatalf("run %s %q: %v", operator, value, err)
		}
		defer rows.Close()
		var found []string
		for rows.Next() {
			var visitor string
			if err := rows.Scan(&visitor); err != nil {
				t.Fatalf("scan: %v", err)
			}
			found = append(found, visitor)
		}
		return found
	}
	same := func(got, want []string) bool {
		if len(got) != len(want) {
			return false
		}
		sort.Strings(got)
		sort.Strings(want)
		for i := range got {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}

	t.Run("segment contains", func(t *testing.T) {
		for _, probe := range []struct {
			operator string
			value    string
			want     []string
		}{
			{"contains", "report_daily", []string{"like-underscore-hit"}},
			{"contains", "%EA%B0%80", []string{"like-percent-hit"}},
			{"not contains", "report_daily", []string{"like-underscore-miss", "like-percent-hit", "like-percent-miss"}},
			{"startsWith", "https://portal.internal/files/%EA", []string{"like-percent-hit"}},
			{"endsWith", "_daily", []string{"like-underscore-hit"}},
		} {
			if got := people(t, probe.operator, probe.value); !same(got, probe.want) {
				t.Errorf("page.url %s %q selected %v, want %v", probe.operator, probe.value, got, probe.want)
			}
		}
	})

	t.Run("visitor search", func(t *testing.T) {
		for query, want := range map[string][]string{
			"report_daily": {"like-underscore-hit"},
			"%EA%B0%80":    {"like-percent-hit"},
		} {
			search := f.get(t, site+"/visitor-search?q="+url.QueryEscape(query)+"&from="+from+"&to="+today)
			results, _ := search["results"].([]any)
			var got []string
			for _, item := range results {
				row, _ := item.(map[string]any)
				got = append(got, row["visitor_id"].(string))
			}
			if !same(got, want) {
				t.Errorf("searching %q found %v, want %v", query, got, want)
			}
		}
	})
}
