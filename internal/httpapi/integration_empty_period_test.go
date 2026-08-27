package httpapi

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Every rate on every screen is a division, and the denominator is a count the
// database returned. A period with nothing in it makes all of them zero at once,
// which is the case a fixture full of activity never reaches — and Go answers a
// division by zero with an infinity rather than a panic, so the failure is not a
// crash but a number that means nothing being displayed as though it did.
//
// This asks every report for a period that is certainly empty and holds the
// answers to what they claim to be: a percentage between nothing and everything,
// a count that is not negative, and a number that exists.
func TestReportsAnswerFiniteNumbersForAnEmptyPeriod(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	site := "/api/v1/sites/" + f.siteKey
	// Well before the fixture's oldest event and only one day wide, so no report
	// can find anything to divide by.
	from, to := f.siteDate(t, -400), f.siteDate(t, -399)
	window := "?from=" + from + "&to=" + to

	for _, path := range []string{
		site + "/overview" + window,
		site + "/visitor-insights" + window,
		site + "/attribution" + window + "&model=last_non_direct",
		site + "/events" + window,
		site + "/pages" + window,
		site + "/sessions" + window,
		site + "/usage" + window,
		site + "/ecommerce" + window,
		site + "/experience" + window,
		site + "/frustration" + window,
		site + "/search-analytics" + window,
		site + "/feature-intelligence" + window,
		site + "/adoption" + window,
		site + "/workspace-rollup" + window,
		site + "/cohort" + window + "&granularity=week&periods=6",
		site + "/path" + window,
		site + "/insights" + window,
		site + "/ai-analytics" + window,
	} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.AddCookie(&http.Cookie{Name: "momento_session", Value: f.sessionCook})
		recorder := httptest.NewRecorder()
		f.server.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Errorf("%s answered %d for an empty period: %s", path, recorder.Code, truncateBody(recorder.Body.String()))
			continue
		}
		var decoded any
		if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
			t.Errorf("%s answered 200 with a body that is not JSON (%v): %s", path, err, truncateBody(recorder.Body.String()))
			continue
		}
		for _, problem := range unreasonableNumbers(decoded, "") {
			t.Errorf("%s: %s", path, problem)
		}
	}
}

// unreasonableNumbers walks a decoded report and reports every number that
// cannot mean what its name says it does. Comparisons against a previous period
// are excluded from the percentage range on purpose: a change can exceed a
// hundred percent and can be negative, which is the one thing a share cannot do.
func unreasonableNumbers(value any, path string) []string {
	problems := []string{}
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			problems = append(problems, unreasonableNumbers(item, join(path, key))...)
		}
	case []any:
		for index, item := range typed {
			problems = append(problems, unreasonableNumbers(item, fmt.Sprintf("%s[%d]", path, index))...)
		}
	case float64:
		name := lastSegment(path)
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return []string{fmt.Sprintf("%s is %v, which no reader can act on", path, typed)}
		}
		if isShare(name) && (typed < 0 || typed > 100) {
			problems = append(problems, fmt.Sprintf("%s is %v, and a share has to be between 0 and 100", path, typed))
		}
		if isCount(name) && typed < 0 {
			problems = append(problems, fmt.Sprintf("%s is %v, and a count cannot be negative", path, typed))
		}
	}
	return problems
}

func isShare(name string) bool {
	if strings.Contains(name, "change") || strings.Contains(name, "delta") || strings.Contains(name, "lift") || strings.Contains(name, "trend") {
		return false
	}
	return strings.HasSuffix(name, "_rate") || strings.HasSuffix(name, "_percent") || name == "rate" || name == "share"
}

func isCount(name string) bool {
	switch name {
	case "users", "sessions", "events", "count", "conversions", "conversion_users", "page_views", "new_users", "buyers", "transactions":
		return true
	}
	return false
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func lastSegment(path string) string {
	if index := strings.LastIndex(path, "."); index >= 0 {
		path = path[index+1:]
	}
	if index := strings.Index(path, "["); index >= 0 {
		path = path[:index]
	}
	return path
}
