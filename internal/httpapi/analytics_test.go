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
// The first version of this guard inspected three expressions and passed. The
// tree holds twenty-eight reads of that property: it matched only those within
// four hundred characters after `event_name='purchase'`, and it never opened
// internal/service, where the digest and the daily rollups compute revenue. A
// guard that reports success over a ninth of its subject is worse than none,
// because it is believed.
//
// So every read is classified and none may be skipped. A read is either paired
// with `revenue` in the same expression, or it belongs to a web vital, where the
// property has no second name. Anything else is the defect this exists for.
func TestEveryRevenueExpressionAcceptsBothPropertyNames(t *testing.T) {
	t.Parallel()

	var files []string
	for _, pattern := range []string{
		filepath.Join(".", "*.go"),
		filepath.Join("..", "insight", "*.go"),
		filepath.Join("..", "service", "*.go"),
		filepath.Join("..", "database", "*.go"),
	} {
		matched, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatalf("list %s: %v", pattern, err)
		}
		files = append(files, matched...)
	}

	// The alias prefix matters: the rollups read e.properties->>'value'.
	read := regexp.MustCompile(`(?:[a-z]+\.)?properties->>'value'`)
	revenue, vitals := 0, 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		text := string(source)
		for _, location := range read.FindAllStringIndex(text, -1) {
			// The window is the expression around the read, which is where the
			// second property name and the metric name both appear.
			start := max(0, location[0]-160)
			end := min(len(text), location[1]+160)
			window := text[start:end]
			line := strings.Count(text[:location[0]], "\n") + 1
			switch {
			case strings.Contains(window, "properties->>'revenue'"):
				revenue++
			case strings.Contains(window, "web_vital"),
				strings.Contains(window, "'metric'"),
				strings.Contains(window, "'rating'"):
				vitals++
			default:
				t.Errorf("%s:%d reads a purchase amount from `value` with no `revenue` beside it and is not a web vital, so a site that sends the other name gets zero here and a number elsewhere:\n%s",
					file, line, window)
			}
		}
	}
	// A floor on both kinds, so a change in how the source is written fails here
	// rather than quietly reducing this to the three expressions it used to see.
	if revenue < 15 {
		t.Fatalf("found only %d revenue expressions, so the pattern no longer matches the source", revenue)
	}
	if vitals < 8 {
		t.Fatalf("found only %d web vital expressions, so the pattern no longer matches the source", vitals)
	}
	t.Logf("checked %d revenue expressions and %d web vital reads across %d files", revenue, vitals, len(files))
}
