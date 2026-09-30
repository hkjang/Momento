package httpapi

import (
	"context"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
)

// The timeline pages backwards with a cursor, and a cursor that is only a
// timestamp loses the events that share it: a page ends in the middle of a run of
// equal timestamps, and the next page asks for events strictly before that time.
// A batch flushed in one beacon or a server-side import writes exactly such runs.
// Following the cursor from the first page to the last has to see every event
// once.
func TestTimelineCursorDoesNotSkipEventsSharingATimestamp(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()
	from, today := f.siteDates(t, 3)
	site := "/api/v1/sites/" + f.siteKey
	const visitor = "cursor-tie-visitor"

	// Five events at one instant, then two earlier ones.
	instant := time.Now().Add(-time.Minute).Truncate(time.Microsecond)
	want := map[string]bool{}
	insert := func(at time.Time) {
		t.Helper()
		id := uuid.NewString()
		if _, err := pool.Exec(ctx, `INSERT INTO raw_events(event_id,site_id,environment,visitor_id,session_id,event_name,event_timestamp,received_at,properties,is_conversion,page_url)
			VALUES($1,$2,'prd',$3,$4,'page_view',$5,$5,'{}'::jsonb,false,'https://portal.internal/tie')`,
			id, f.siteID, visitor, visitor+"-s", at); err != nil {
			t.Fatalf("insert: %v", err)
		}
		want[id] = true
	}
	for range 5 {
		insert(instant)
	}
	insert(instant.Add(-2 * time.Second))
	insert(instant.Add(-3 * time.Second))

	base := site + "/visitors/" + visitor + "/timeline?scope=device&limit=2&from=" + from + "&to=" + today
	collect := func(t *testing.T, withID bool) map[string]int {
		t.Helper()
		seen := map[string]int{}
		cursor := ""
		for page := 0; page < 10; page++ {
			trace := f.get(t, base+cursor)
			sessions, _ := trace["sessions"].([]any)
			for _, raw := range sessions {
				session, _ := raw.(map[string]any)
				events, _ := session["events"].([]any)
				for _, rawEvent := range events {
					event, _ := rawEvent.(map[string]any)
					seen[fmt.Sprint(event["event_id"])]++
				}
			}
			paging, _ := trace["paging"].(map[string]any)
			if more, _ := paging["has_more"].(bool); !more {
				return seen
			}
			cursor = "&before=" + url.QueryEscape(fmt.Sprint(paging["next_before"]))
			if withID {
				id, _ := paging["next_before_id"].(string)
				if id == "" {
					t.Fatalf("a page with more after it gave no next_before_id: %v", paging)
				}
				cursor += "&before_id=" + url.QueryEscape(id)
			}
		}
		t.Fatal("the cursor never reached the end of a seven-event trace")
		return nil
	}

	t.Run("the (timestamp, id) cursor sees every event once", func(t *testing.T) {
		seen := collect(t, true)
		if len(seen) != len(want) {
			t.Fatalf("paged through %d distinct events, want %d", len(seen), len(want))
		}
		for id, count := range seen {
			if !want[id] {
				t.Errorf("event %s is not one this test wrote", id)
			}
			if count != 1 {
				t.Errorf("event %s appeared on %d pages", id, count)
			}
		}
	})

	// The case the id fixes, kept visible: the timestamp alone drops the rest of
	// the run the first page ended in. If this ever passes with every event, the
	// fixture no longer builds a boundary tie and the test above proves nothing.
	t.Run("the timestamp alone skips the rest of the tie", func(t *testing.T) {
		if seen := collect(t, false); len(seen) >= len(want) {
			t.Fatalf("timestamp-only paging saw %d of %d events; the fixture has no boundary tie", len(seen), len(want))
		}
	})

	t.Run("a malformed before_id is refused", func(t *testing.T) {
		response := f.rawGet(t, base+"&before="+url.QueryEscape(instant.UTC().Format(time.RFC3339Nano))+"&before_id=abc", "")
		if response.Code != 400 {
			t.Fatalf("status = %d, want 400: %s", response.Code, response.Body.String())
		}
	})
}
