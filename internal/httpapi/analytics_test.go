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
