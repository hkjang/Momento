package httpapi

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hkjang/Momento/internal/service"
)

// The export is opened in a spreadsheet, and a spreadsheet does not read a cell
// that starts with "=" as text. Nearly every column is written by a visitor's
// browser, so whoever can reach the collector can decide what the analyst's
// Excel evaluates.
func TestACSVCellIsNeverAFormula(t *testing.T) {
	t.Parallel()

	// The four characters the spreadsheets evaluate, with and without the
	// whitespace an importer might trim first.
	for _, hostile := range []string{
		`=HYPERLINK("https://evil.example","open")`, `+1+1`, `-2+3`, `@SUM(A1:A9)`,
		` =cmd`, "\t=cmd", "\r\n=cmd",
	} {
		got := csvCell(hostile)
		if !strings.HasPrefix(got, "'") {
			t.Errorf("csvCell(%q) = %q: the cell is still evaluated as a formula", hostile, got)
		}
		if got[1:] != hostile {
			t.Errorf("csvCell(%q) = %q: the value itself changed", hostile, got)
		}
	}

	// Everything the export actually carries comes through untouched: ids,
	// timestamps, URLs, JSON, empty cells, and the note on the last line.
	for _, plain := range []string{
		"", "page_view", "2026-09-12 03:31:16 +0000 UTC", "https://portal.internal/home?q=a",
		`{"feature":"=search"}`, "3f2a9c1e-0000-4000-8000-000000000000", "#momento", "1",
	} {
		if got := csvCell(plain); got != plain {
			t.Errorf("csvCell(%q) = %q: a harmless cell was changed", plain, got)
		}
	}
}

// The same thing end to end: a campaign a visitor's browser sends comes out of
// the CSV export as text. The test reads the file back with a CSV reader so it
// checks the cell the spreadsheet would see, not a substring of the body.
func TestTheExportedCSVCannotCarryAFormula(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	from, today := f.siteDates(t, 30)
	site := "/api/v1/sites/" + f.siteKey

	hostile := `=HYPERLINK("https://evil.example","open")`
	payload := fmt.Sprintf(`{"site_id":"%s","environment":"prd","tracking_key":"%s","visitor_id":"visitor-formula","session_id":"session-formula",
		"context":{"page":{"url":"https://portal.internal/home","title":"홈","referrer":""},"device":{"browser":"Chrome","os":"Windows","type":"desktop"},"traffic":{"source":"intranet","medium":"portal","campaign":%q}},
		"events":[{"id":"%s","name":"formula_check","timestamp":%d,"properties":{},"contract_version":1}]}`,
		f.siteKey, f.trackingKey, hostile, uuid.NewString(), time.Now().UnixMilli())
	request := httptest.NewRequest(http.MethodPost, "/collect/v1/events", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://portal.internal")
	recorder := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("collect = %d: %s", recorder.Code, recorder.Body.String())
	}
	if err := (service.Worker{DB: f.server.DB}).ProcessPending(context.Background()); err != nil {
		t.Fatalf("drain the inbox: %v", err)
	}

	request = httptest.NewRequest(http.MethodGet, site+"/export?from="+from+"&to="+today, nil)
	request.AddCookie(&http.Cookie{Name: "momento_session", Value: f.sessionCook})
	recorder = httptest.NewRecorder()
	f.server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("export = %d: %s", recorder.Code, recorder.Body.String())
	}
	records, err := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(recorder.Body.Bytes(), []byte{0xEF, 0xBB, 0xBF}))).ReadAll()
	if err != nil {
		t.Fatalf("read the export back: %v", err)
	}
	campaign := -1
	for i, name := range records[0] {
		if name == "campaign" {
			campaign = i
		}
	}
	if campaign < 0 {
		t.Fatalf("the export has no campaign column: %v", records[0])
	}
	found := false
	for _, record := range records[1:] {
		for _, cell := range record {
			if trimmed := strings.TrimLeft(cell, " \t\r\n"); trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0])) {
				t.Errorf("the export carries a cell a spreadsheet would evaluate: %q", cell)
			}
		}
		if record[campaign] == "'"+hostile {
			found = true
		}
	}
	if !found {
		t.Fatalf("the campaign the visitor sent did not reach the export as text: %q", hostile)
	}
}
