package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// waitFor polls until cond holds, so a test can line requests up behind a leader
// without guessing at a sleep.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition was not reached in five seconds")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestIdenticalReadsInFlightRunOnce(t *testing.T) {
	var group flightGroup
	var runs atomic.Int32
	release := make(chan struct{})
	serve := func(w http.ResponseWriter) {
		runs.Add(1)
		<-release
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"users":42}`))
	}

	const callers = 5
	recorders := make([]*httptest.ResponseRecorder, callers)
	shared := make([]bool, callers)
	var wg sync.WaitGroup
	start := func(i int) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			recorders[i] = httptest.NewRecorder()
			shared[i] = group.do(context.Background(), "k", recorders[i], serve)
		}()
	}
	start(0)
	waitFor(t, func() bool { return runs.Load() == 1 })
	for i := 1; i < callers; i++ {
		start(i)
	}
	waitFor(t, func() bool { return group.waiting("k") == callers-1 })
	close(release)
	wg.Wait()

	if got := runs.Load(); got != 1 {
		t.Fatalf("the read ran %d times for %d identical requests, want once", got, callers)
	}
	for i, recorder := range recorders {
		if recorder.Code != 200 || recorder.Body.String() != `{"users":42}` || recorder.Header().Get("Content-Type") != "application/json" {
			t.Fatalf("caller %d got %d %q %v", i, recorder.Code, recorder.Body.String(), recorder.Header())
		}
		if shared[i] != (i > 0) || (recorder.Header().Get("X-Momento-Shared-Read") == "1") != (i > 0) {
			t.Fatalf("caller %d shared=%v header=%q", i, shared[i], recorder.Header().Get("X-Momento-Shared-Read"))
		}
	}

	// It is not a cache: once the read is over, the next request reads again.
	next := httptest.NewRecorder()
	if group.do(context.Background(), "k", next, func(w http.ResponseWriter) {
		runs.Add(1)
		_, _ = w.Write([]byte(`{"users":43}`))
	}) {
		t.Fatal("a request after the read finished was given the old answer")
	}
	if next.Body.String() != `{"users":43}` || runs.Load() != 2 {
		t.Fatalf("the next request got %q after %d runs", next.Body.String(), runs.Load())
	}
}

func TestACancelledLeaderDoesNotAnswerForOthers(t *testing.T) {
	var group flightGroup
	var runs atomic.Int32
	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	started := make(chan struct{})
	leaderDone := make(chan struct{})
	go func() {
		defer close(leaderDone)
		group.do(leaderCtx, "k", httptest.NewRecorder(), func(w http.ResponseWriter) {
			runs.Add(1)
			close(started)
			<-leaderCtx.Done()
			w.WriteHeader(http.StatusInternalServerError)
		})
	}()
	<-started
	follower := httptest.NewRecorder()
	followerDone := make(chan bool)
	go func() {
		followerDone <- group.do(context.Background(), "k", follower, func(w http.ResponseWriter) {
			runs.Add(1)
			_, _ = w.Write([]byte("fresh"))
		})
	}()
	waitFor(t, func() bool { return group.waiting("k") == 1 })
	cancelLeader()
	<-leaderDone
	if shared := <-followerDone; shared {
		t.Fatal("the waiting request was handed the cancelled leader's response")
	}
	if follower.Code != 200 || follower.Body.String() != "fresh" || runs.Load() != 2 {
		t.Fatalf("the waiting request got %d %q after %d runs, want its own read", follower.Code, follower.Body.String(), runs.Load())
	}
}

func TestSegmentQueriesAreNeverShared(t *testing.T) {
	for query, want := range map[string]bool{
		"from=2026-09-01&to=2026-09-30":       true,
		"segment_id=abc":                      false,
		"segment_ids=a,b":                     false,
		"from=2026-09-01&Segment=x":           false,
		"environment=prd&segments=%5B%5D&x=1": false,
	} {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/sites/S/overview?"+query, nil)
		if got := shareableQuery(request); got != want {
			t.Errorf("%s: shareable = %v, want %v", query, got, want)
		}
	}
}
