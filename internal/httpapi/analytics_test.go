package httpapi

import (
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
