package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Visitor Explorer is four surfaces: the profile list, the identity report, the
// timeline and the visitor search, plus the query_identity_graph MCP tool. Each
// of them asked the privacy policy whether the feature was allowed, and they
// asked with their own inline SQL and their own fallback. Two files defaulted
// visitor_profiles to true and three to false.
//
// So a stored policy that does not name the field — a write that sent only the
// fields it changed, a row from before the field existed — left half the feature
// answering and the other half reporting that an administrator had switched it
// off. There is no error to find: both halves are behaving exactly as written.
func TestVisitorExplorerAgreesWithItselfAboutBeingSwitchedOn(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()

	var previous []byte
	if err := pool.QueryRow(ctx, `SELECT value FROM settings WHERE key='privacy'`).Scan(&previous); err != nil {
		t.Fatalf("read the privacy policy: %v", err)
	}
	// The state under test: a policy that simply does not carry the field.
	if _, err := pool.Exec(ctx, `UPDATE settings SET value=value-'visitor_profiles' WHERE key='privacy'`); err != nil {
		t.Fatalf("remove visitor_profiles: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `UPDATE settings SET value=$1::jsonb WHERE key='privacy'`, previous); err != nil {
			t.Fatalf("restore the privacy policy: %v", err)
		}
	})
	var stored map[string]any
	if err := json.Unmarshal(previous, &stored); err != nil {
		t.Fatalf("decode the stored policy: %v", err)
	}
	if allowed, _ := stored["visitor_profiles"].(bool); !allowed {
		t.Fatal("the fixture ships with Visitor Explorer disabled, so removing the field proves nothing")
	}

	from, today := f.siteDates(t, 30)
	site := "/api/v1/sites/" + f.siteKey
	window := "?from=" + from + "&to=" + today
	surfaces := map[string]int{
		site + "/visitors" + window:                              0,
		site + "/identities" + window:                            0,
		site + "/visitor-search?q=" + f.userID:                   0,
		site + "/visitors/" + f.visitorID + "/timeline" + window: 0,
	}
	for path := range surfaces {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.AddCookie(&http.Cookie{Name: "momento_session", Value: f.sessionCook})
		recorder := httptest.NewRecorder()
		f.server.Handler().ServeHTTP(recorder, request)
		surfaces[path] = recorder.Code
	}

	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"query_identity_graph","arguments":{"site_id":%q}}}`, f.siteKey)
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: "momento_session", Value: f.sessionCook})
	recorder := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(recorder, request)
	var envelope map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode the tool response: %v", err)
	}
	result, _ := envelope["result"].(map[string]any)
	toolRefused, _ := result["isError"].(bool)

	refused := []string{}
	answered := []string{}
	for path, code := range surfaces {
		if code == http.StatusForbidden {
			refused = append(refused, path)
		} else if code == http.StatusOK {
			answered = append(answered, path)
		} else {
			t.Fatalf("%s = %d, which is neither an answer nor a refusal", path, code)
		}
	}
	if toolRefused {
		refused = append(refused, "query_identity_graph")
	} else {
		answered = append(answered, "query_identity_graph")
	}
	if len(refused) > 0 && len(answered) > 0 {
		t.Fatalf("the same policy switched Visitor Explorer off for %v and left it on for %v: an operator sees half a feature and a refusal that names a policy nobody wrote",
			refused, answered)
	}
	// And the direction has to be the shipped one. This product ships with
	// Visitor Explorer on, so a policy that says nothing about it must not read
	// as an administrator having turned it off.
	if len(refused) > 0 {
		t.Fatalf("a policy that does not name visitor_profiles refused every surface: absent is being read as off, and this product ships it on")
	}
}

// A settings write names the fields it changes. It used to replace the whole
// group, so a caller that sent four privacy fields dropped the masked
// parameters, the blocked properties and the PII mode — and downstream every
// one of those absences reads as "do not protect this".
//
// The console always sent the whole object, so nothing on the screen showed the
// hole. The API is the surface, and a partial write to it is an ordinary thing
// to do.
func TestAPartialSettingsWriteDoesNotDropTheFieldsItDoesNotName(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()

	var previous []byte
	var previousAuthor *uuid.UUID
	// updated_by too: this test writes through the API, which stamps the
	// fixture's administrator, and the next seed deletes that user.
	if err := pool.QueryRow(ctx, `SELECT value,updated_by FROM settings WHERE key='privacy'`).Scan(&previous, &previousAuthor); err != nil {
		t.Fatalf("read the privacy policy: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `UPDATE settings SET value=$1::jsonb,updated_by=$2 WHERE key='privacy'`, previous, previousAuthor); err != nil {
			t.Fatalf("restore the privacy policy: %v", err)
		}
	})
	var before map[string]any
	if err := json.Unmarshal(previous, &before); err != nil {
		t.Fatalf("decode the stored policy: %v", err)
	}
	for _, field := range []string{"masked_parameters", "blocked_properties", "pii_detection_mode", "do_not_track"} {
		if _, ok := before[field]; !ok {
			t.Fatalf("the fixture policy does not carry %s, so this proves nothing", field)
		}
	}

	f.do(t, http.MethodPut, "/api/v1/settings/privacy", `{"ip_anonymization":false}`)

	var updated []byte
	if err := pool.QueryRow(ctx, `SELECT value FROM settings WHERE key='privacy'`).Scan(&updated); err != nil {
		t.Fatalf("read the policy back: %v", err)
	}
	var after map[string]any
	if err := json.Unmarshal(updated, &after); err != nil {
		t.Fatalf("decode the written policy: %v", err)
	}
	if anonymised, _ := after["ip_anonymization"].(bool); anonymised {
		t.Fatal("the field the write named did not change, so the merge is not writing anything")
	}
	for field, want := range before {
		if field == "ip_anonymization" {
			continue
		}
		got, ok := after[field]
		if !ok {
			t.Errorf("%s is gone after a write that never mentioned it: downstream that absence is read as the protection being switched off", field)
			continue
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("%s changed from %v to %v in a write that never mentioned it", field, want, got)
		}
	}
}
