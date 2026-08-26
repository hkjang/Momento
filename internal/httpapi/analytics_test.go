package httpapi

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestPreviousDateRangeKeepsCalendarMidnightAcrossDST(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 3, 1, 0, 0, 0, 0, location).UTC()
	to := time.Date(2026, 4, 1, 0, 0, 0, 0, location).UTC()
	previousFrom, previousTo := previousDateRange(from, to, location)
	if got := previousFrom.In(location).Format(time.RFC3339); got != "2026-01-29T00:00:00-05:00" {
		t.Fatalf("previous from = %s", got)
	}
	if got := previousTo.In(location).Format(time.RFC3339); got != "2026-03-01T00:00:00-05:00" {
		t.Fatalf("previous to = %s", got)
	}
}

func TestLocalDateBucketRange(t *testing.T) {
	location, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, location).UTC()
	to := time.Date(2026, 8, 9, 0, 0, 0, 0, location).UTC()
	dateFrom, dateTo, ok := localDateBucketRange(from, to, location)
	if !ok || dateFrom.Format("2006-01-02") != "2026-08-01" || dateTo.Format("2006-01-02") != "2026-08-09" {
		t.Fatalf("unexpected daily range: %s %s %v", dateFrom, dateTo, ok)
	}
	if _, _, ok := localDateBucketRange(from.Add(time.Minute), to, location); ok {
		t.Fatal("partial-day range must not use daily aggregates")
	}
}

func TestNormalizePathView(t *testing.T) {
	t.Parallel()

	for input, want := range map[string]string{
		"":       "all",
		"all":    "all",
		"pages":  "pages",
		"events": "events",
	} {
		got, err := normalizePathView(input)
		if err != nil || got != want {
			t.Fatalf("normalizePathView(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	if _, err := normalizePathView("sessions"); err == nil {
		t.Fatal("unsupported path view must return an error")
	}
}

// TestEveryRevenueExpressionAcceptsBothPropertyNames guards a class of defect
// rather than one instance. The tracker lets a purchase carry its amount as
// either `value` or `revenue`, and the reports read both — except the query
// builder, which read only `value` and answered zero for a site that sends the
// other. Nothing failed; the number was simply wrong on one screen.
//
// Reading the source is crude, but the alternative is a per-report test that
// would not exist for the next report someone adds.
func TestEveryRevenueExpressionAcceptsBothPropertyNames(t *testing.T) {
	t.Parallel()

	files, err := filepath.Glob(filepath.Join(".", "*.go"))
	if err != nil {
		t.Fatalf("list sources: %v", err)
	}
	insight, err := filepath.Glob(filepath.Join("..", "insight", "*.go"))
	if err != nil {
		t.Fatalf("list insight sources: %v", err)
	}
	files = append(files, insight...)

	// Each occurrence of a purchase amount is checked in the window around it,
	// which is where the property names appear.
	purchase := regexp.MustCompile(`event_name='purchase'[^;]{0,400}`)
	checked := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		for _, match := range purchase.FindAllString(string(source), -1) {
			if !strings.Contains(match, "properties->>'value'") {
				continue
			}
			checked++
			if !strings.Contains(match, "properties->>'revenue'") {
				t.Errorf("%s reads a purchase amount from `value` without `revenue`, so a site that sends the other name gets zero here and a number elsewhere:\n%s", file, match)
			}
		}
	}
	if checked == 0 {
		t.Fatal("found no purchase amount expressions, so the pattern no longer matches the source")
	}
}
