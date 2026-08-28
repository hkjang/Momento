package httpapi

import (
	"context"
	"testing"
)

// The data quality screen reports the ingestion backlog: how many events are
// waiting to be processed and how long the oldest has waited. An operator reads
// those two numbers to decide whether collection is healthy.
//
// The query selected a column event_inbox does not have. It has never worked —
// and nobody could tell, because the error was discarded, so the screen reported
// a backlog of zero waiting zero seconds. The one answer a health screen must
// never give by default is "everything is fine", and that is the only answer this
// one could give.
//
// This puts real work in the inbox and requires the screen to see it.
func TestTheDataQualityScreenReportsTheIngestionBacklog(t *testing.T) {
	pool := testPool(t)
	f := seed(t, pool)
	ctx := context.Background()

	// Events that have arrived and not yet been processed: exactly what the
	// backlog counts. They are left unprocessed on purpose.
	const waiting = 3
	for index := 0; index < waiting; index++ {
		if _, err := pool.Exec(ctx, `INSERT INTO event_inbox(site_id,payload,created_at) VALUES($1,$2,now()-interval '90 seconds')`,
			f.siteID, []byte(`{"events":[]}`)); err != nil {
			t.Fatalf("seed the inbox: %v", err)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM event_inbox WHERE site_id=$1 AND processed_at IS NULL`, f.siteID)
	})

	from, today := f.siteDates(t, 30)
	report := f.get(t, "/api/v1/sites/"+f.siteKey+"/data-quality?from="+from+"&to="+today)
	ingestion, _ := report["collector"].(map[string]any)
	if ingestion == nil {
		t.Fatalf("the data quality report has no collector section: %v", report)
	}
	pending, ok := ingestion["pending"].(float64)
	if !ok {
		t.Fatalf("the report does not say how many events are waiting: %v", ingestion)
	}
	if int(pending) < waiting {
		t.Errorf("%d events are waiting in the inbox and the screen reports %v — an operator reading this decides collection is healthy",
			waiting, ingestion["pending"])
	}
	lag, ok := ingestion["inbox_lag_seconds"].(float64)
	if !ok {
		t.Fatalf("the report does not say how long the oldest event has waited: %v", ingestion)
	}
	if lag < 60 {
		t.Errorf("the oldest waiting event is 90 seconds old and the screen reports a lag of %vs", lag)
	}
}
