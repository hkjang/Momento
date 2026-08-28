package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hkjang/Momento/internal/service"
)

// The workspace rollup exists to count a person once across services. Its own
// comment says so: "A person seen on two services is one person here, which is
// the whole point of the rollup, so this cannot be summed from the table above."
//
// The response carries both numbers — users, deduplicated across the workspace,
// and site_user_sum, the per-service counts added up — so the difference between
// them is the answer the screen is for. If somebody ever computed users by
// summing the services, the two would become equal and nothing would look wrong:
// the screen would still show two plausible numbers, and the one it exists to
// produce would silently be the one it was built to replace.
//
// The fixture has an SSO user active on both services, so the difference is not
// a rounding detail here — it is one whole person.
func TestTheWorkspaceRollupCountsAPersonOnce(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()

	// The fixture describes itself as "two services in one workspace, an SSO user
	// active on both", and the second service has no events at all — so the case
	// this report exists for has never been exercised by anything. One person
	// working across both services, and one person on each service alone, so the
	// deduplicated count and the per-service sum have to differ by exactly one.
	now := time.Now()
	visit := func(siteKey, user, visitor string) {
		t.Helper()
		event := fmt.Sprintf(`{"id":%q,"name":"page_view","timestamp":%d,"properties":{},"contract_version":1}`,
			uuid.NewString(), now.UnixMilli())
		payload := fmt.Sprintf(`{"site_id":%q,"environment":"prd","tracking_key":%q,"visitor_id":%q,"session_id":%q,"user_id":%q,
			"context":{"page":{"url":"https://portal.internal/rollup","title":"페이지","referrer":""},"device":{"browser":"Chrome","os":"Windows","type":"desktop"},"traffic":{"source":"intranet","medium":"portal"}},
			"events":[%s]}`, siteKey, f.trackingKey, visitor, "rollup-"+visitor+"-"+siteKey, user, event)
		request := httptest.NewRequest(http.MethodPost, "/collect/v1/events", strings.NewReader(payload))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "https://portal.internal")
		recorder := httptest.NewRecorder()
		f.server.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusAccepted {
			t.Fatalf("collect for %s: %d %s", siteKey, recorder.Code, truncateBody(recorder.Body.String()))
		}
	}
	visit(f.siteKey, "ROLLUP_BOTH", "rollup-both-portal")
	visit(f.otherKey, "ROLLUP_BOTH", "rollup-both-hr")
	visit(f.siteKey, "ROLLUP_PORTAL", "rollup-portal-only")
	visit(f.otherKey, "ROLLUP_HR", "rollup-hr-only")
	if err := (service.Worker{DB: pool}).ProcessPending(ctx); err != nil {
		t.Fatalf("drain the inbox: %v", err)
	}

	from, today := f.siteDates(t, 30)

	report := f.get(t, "/api/v1/sites/"+f.siteKey+"/workspace-rollup?from="+from+"&to="+today)
	summary, _ := report["summary"].(map[string]any)
	if summary == nil {
		t.Fatalf("the rollup answered no summary: %v", report)
	}
	services, _ := report["services"].([]any)
	if len(services) < 2 {
		t.Fatalf("the workspace has %d service(s) in this period, so counting a person once across services proves nothing", len(services))
	}

	unique, uniqueOK := summary["users"].(float64)
	sum, sumOK := summary["site_user_sum"].(float64)
	if !uniqueOK || !sumOK {
		t.Fatalf("the summary does not carry both counts: %v", summary)
	}
	if unique <= 0 {
		t.Fatalf("the rollup reports %v people, so there is nothing to deduplicate", unique)
	}
	if unique > sum {
		t.Errorf("the workspace reports %v people and its services add up to %v: a person counted once cannot outnumber the same people counted per service",
			unique, sum)
	}
	// And the per-service counts have to add up to what the report says they do,
	// or site_user_sum is measuring something other than the rows beside it.
	added := 0.0
	for _, row := range services {
		service, _ := row.(map[string]any)
		if service == nil {
			continue
		}
		added += toNumber(service["users"])
	}
	if added != sum {
		t.Errorf("the service rows add up to %v and the summary says %v", added, sum)
	}
	// The fixture's SSO user is active on both services, so the deduplicated
	// count must actually be smaller. Without this the test would pass on a
	// rollup that had quietly become the sum.
	if unique >= sum {
		t.Errorf("the workspace reports %v people and the services %v: the fixture has one person on both services, so counting once has to give a smaller number — this is what the rollup is for",
			unique, sum)
	}
	t.Logf("%v people across the workspace, %v counted per service", unique, sum)
}
