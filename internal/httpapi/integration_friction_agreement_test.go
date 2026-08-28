package httpapi

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hkjang/Momento/internal/service"
)

// The frustration screen answers from three queries over the same window: the
// per-signal list, the impact comparison, and the audiences. Two of them count
// the same thing in different ways.
//
// The signal list counts DISTINCT entity_id per event name. The impact report
// builds its own per-person aggregate and joins it to the people who touched
// each signal. Those two numbers are the same population arrived at twice, and
// frictionImpactReport says of itself that "the person aggregate is built once
// and both the totals and the per-signal counts are read from it, so the two
// sides always add up to the same population" — which is a claim about that
// report's internals and says nothing about the list beside it on the screen.
//
// A reader sees both. If they drift, one row says nine people hit a signal and
// the row below says seven were affected by it, and nothing on the screen
// explains the difference.
func TestTheFrustrationScreenAgreesWithItself(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()

	// Friction the fixture does not otherwise carry, so the comparison is made
	// against numbers that exist: two people, different signals, one of them
	// converting so the impact report has both populations.
	now := time.Now()
	deliver := func(visitor string, events ...string) {
		t.Helper()
		parts := make([]string, 0, len(events))
		for index, name := range events {
			parts = append(parts, fmt.Sprintf(`{"id":%q,"name":%q,"timestamp":%d,"properties":{},"contract_version":1}`,
				uuid.NewString(), name, now.Add(time.Duration(index)*time.Second).UnixMilli()))
		}
		f.postCollect(t, f.siteKey, "friction-"+visitor, visitor, "https://portal.internal/app", parts)
	}
	deliver("friction-one", "rage_click", "rage_click", "dead_click")
	deliver("friction-two", "rage_click", "form_retry")
	deliver("friction-three", "dead_click")
	if err := (service.Worker{DB: pool}).ProcessPending(ctx); err != nil {
		t.Fatalf("drain the inbox: %v", err)
	}

	from, today := f.siteDates(t, 1)
	report := f.get(t, "/api/v1/sites/"+f.siteKey+"/frustration?from="+from+"&to="+today)

	signals, _ := report["signals"].([]any)
	impact, _ := report["impact"].([]any)
	if len(signals) == 0 {
		t.Fatal("the screen lists no signals, so there is nothing to hold the impact report to")
	}
	if len(impact) == 0 {
		t.Fatal("the screen reports no impact, so there is nothing to compare the signals with")
	}

	affected := map[string]float64{}
	for _, row := range impact {
		item, _ := row.(map[string]any)
		if item == nil {
			continue
		}
		affected[fmt.Sprint(item["signal"])] = toNumber(item["affected_people"])
	}

	compared := 0
	for _, row := range signals {
		item, _ := row.(map[string]any)
		if item == nil {
			continue
		}
		name := fmt.Sprint(item["signal"])
		users := toNumber(item["users"])
		reported, ok := affected[name]
		if !ok {
			t.Errorf("the signal list shows %s and the impact report does not mention it: a reader sees one row and not the other", name)
			continue
		}
		if users == 0 {
			continue
		}
		compared++
		if users != reported {
			t.Errorf("%s: the signal list says %v people hit it and the impact report says %v were affected — the same population counted twice, and the screen shows both rows",
				name, users, reported)
		}
	}
	if compared == 0 {
		t.Fatal("no signal had a non-zero population, so agreement proves nothing")
	}
	t.Logf("compared %d signals between the list and the impact report", compared)
}

// toNumber reads a JSON number however the encoder wrote it.
func toNumber(value any) float64 {
	if number, ok := value.(float64); ok {
		return number
	}
	return 0
}
