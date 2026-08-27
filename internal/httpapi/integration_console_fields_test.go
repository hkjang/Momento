package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The console tells a reader when a screen is empty because nothing happened
// and when it is empty because nothing is being measured. Those advisories are
// decided in the browser, from fields of these responses — summary.transactions,
// summary.revenue, population, rows[].label and the rest.
//
// A renamed field does not break anything visibly. The advisory simply stops
// appearing, and the screen goes back to showing an unexplained table of zeroes
// — which is the state it was in before, so nobody notices it returned. The
// console's own tests cannot catch it either: they build the objects by hand,
// so they agree with whatever the test author believed the field was called. I
// made exactly that mistake writing one of them, and only a real response
// showed it.
//
// This is the response saying the names out loud.
func TestTheFieldsTheConsoleReadsForItsSetupAdvice(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	from, today := f.siteDates(t, 30)
	site := "/api/v1/sites/" + f.siteKey

	for _, probe := range []struct {
		screen string
		path   string
		// object field -> the keys the console reads off it
		objects map[string][]string
		// list field -> the keys the console reads off each row
		lists map[string][]string
		// fields the console reads directly off the answer
		fields []string
	}{
		{
			screen:  "ecommerce",
			path:    site + "/ecommerce?from=" + from + "&to=" + today,
			objects: map[string][]string{"summary": {"transactions", "revenue"}},
			lists:   map[string][]string{"funnel": {"event", "users"}, "products": nil},
		},
		{
			screen: "feature intelligence",
			path:   site + "/feature-intelligence?from=" + from + "&to=" + today,
			fields: []string{"population"},
			lists:  map[string][]string{"features": nil},
		},
		{
			screen: "AI operations",
			path:   site + "/ai-analytics?from=" + from + "&to=" + today + "&group_by=model",
			lists:  map[string][]string{"rows": {"label"}},
		},
	} {
		request := httptest.NewRequest(http.MethodGet, probe.path, nil)
		request.AddCookie(&http.Cookie{Name: "momento_session", Value: f.sessionCook})
		recorder := httptest.NewRecorder()
		f.server.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Errorf("%s answered %d: %s", probe.screen, recorder.Code, truncateBody(recorder.Body.String()))
			continue
		}
		var answer map[string]any
		if err := json.Unmarshal(recorder.Body.Bytes(), &answer); err != nil {
			t.Errorf("%s: %v", probe.screen, err)
			continue
		}
		for _, field := range probe.fields {
			if _, ok := answer[field]; !ok {
				t.Errorf("%s no longer answers with %q, which the console reads to tell an empty screen from an unmeasured one", probe.screen, field)
			}
		}
		for field, keys := range probe.objects {
			object, ok := answer[field].(map[string]any)
			if !ok {
				t.Errorf("%s no longer answers with a %q object", probe.screen, field)
				continue
			}
			for _, key := range keys {
				if _, ok := object[key]; !ok {
					t.Errorf("%s: %s.%s is gone, and the advisory that reads it will simply stop appearing", probe.screen, field, key)
				}
			}
		}
		for field, keys := range probe.lists {
			list, ok := answer[field].([]any)
			if !ok {
				t.Errorf("%s no longer answers with a %q list", probe.screen, field)
				continue
			}
			if len(keys) == 0 {
				continue
			}
			if len(list) == 0 {
				t.Errorf("%s: %q is empty in the fixture, so the keys the console reads off it cannot be checked here", probe.screen, field)
				continue
			}
			row, ok := list[0].(map[string]any)
			if !ok {
				t.Errorf("%s: %q holds something other than rows", probe.screen, field)
				continue
			}
			for _, key := range keys {
				if _, ok := row[key]; !ok {
					t.Errorf("%s: %s[].%s is gone, and the advisory that reads it will simply stop appearing", probe.screen, field, key)
				}
			}
		}
	}
}
