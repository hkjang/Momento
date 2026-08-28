package httpapi

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hkjang/Momento/internal/service"
)

// The tracker's offline queue puts a failed batch back whole, including the
// groups it had already sent, and says why that is safe: "Redelivery is safe:
// every event carries an id and the collector ignores one it has."
//
// Nothing had ever delivered the same event twice. That claim is the whole
// design of the retry path — a reconnection re-sends what it already sent — and
// if the collector stopped ignoring a repeat, everyone who went offline and came
// back would double their own numbers on the way. It would not look like a
// defect either: more events from real people at a plausible moment.
//
// So this delivers a batch, then delivers the identical batch again, and
// requires the site to report the same numbers and to say that it saw the
// repeats.
func TestRedeliveringABatchChangesNothingButTheDuplicateCount(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()
	site := "/api/v1/sites/" + f.siteKey

	// A batch of its own, with ids fixed up front so the second delivery is the
	// same events rather than new ones that look alike.
	now := time.Now()
	const events = 4
	ids := make([]string, events)
	for index := range ids {
		ids[index] = uuid.NewString()
	}
	batch := make([]string, 0, events)
	for index, id := range ids {
		conversion := ""
		name := "page_view"
		if index == events-1 {
			name, conversion = "purchase", `"value":"1000",`
		}
		batch = append(batch, fmt.Sprintf(`{"id":%q,"name":%q,"timestamp":%d,"properties":{%s"feature":"재전송"},"contract_version":1}`,
			id, name, now.Add(time.Duration(index)*time.Second).UnixMilli(), conversion))
	}
	send := func() {
		t.Helper()
		f.postCollect(t, f.siteKey, "redeliver-session", "redeliver-visitor", "https://portal.internal/retry", batch)
		if err := (service.Worker{DB: pool}).ProcessPending(ctx); err != nil {
			t.Fatalf("drain the inbox: %v", err)
		}
	}

	from, today := f.siteDates(t, 1)
	measure := func() (map[string]any, map[string]any) {
		t.Helper()
		overview, _ := f.get(t, site+"/overview?from="+from+"&to="+today)["current"].(map[string]any)
		quality, _ := f.get(t, site+"/data-quality?from="+from+"&to="+today)["quality"].(map[string]any)
		if overview == nil || quality == nil {
			t.Fatalf("could not read the site's numbers: overview=%v quality=%v", overview != nil, quality != nil)
		}
		return overview, quality
	}

	send()
	afterFirst, qualityFirst := measure()
	// The delivery has to have moved something, or a second one changing nothing
	// says nothing.
	if toNumber(afterFirst["events"]) == 0 {
		t.Fatal("the first delivery left the site reporting no events")
	}

	send()
	afterSecond, qualitySecond := measure()

	for _, measure := range []string{"users", "sessions", "events", "page_views", "conversions", "conversion_users", "revenue"} {
		before, after := toNumber(afterFirst[measure]), toNumber(afterSecond[measure])
		if before != after {
			t.Errorf("%s went from %v to %v when the same events were delivered again: a person who reconnects doubles their own numbers, and it looks like real traffic",
				measure, before, after)
		}
	}

	// And the repeats are reported rather than silently dropped, because an
	// operator watching a client retry badly has no other way to see it.
	seen := toNumber(qualitySecond["duplicates"]) - toNumber(qualityFirst["duplicates"])
	if seen != events {
		t.Errorf("the second delivery repeated %d events and the data quality screen counted %v: a client stuck in a retry loop is invisible at this number",
			events, seen)
	}
}
