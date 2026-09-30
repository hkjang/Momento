package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"sync"
)

// Identical analytical reads that arrive together are answered by one query.
//
// The reports read millions of events and take seconds; the landing screen fires
// several of them at once, and on an internal service a team opens it at the
// same time — the Monday stand-up, a link pasted into a channel. Each of those
// requests ran the same scans side by side, and a single request already fans
// out to eight connections, so four people opening the overview together could
// hold the whole pool while computing one answer four times.
//
// This is not a cache. A request only joins a read that is already running,
// so every answer it receives was computed after it arrived: nothing can be
// older than the request that asked. Once the read finishes, the next request
// starts a new one.
//
// Sharing is only safe where the answer depends on nothing but the site and the
// request itself. The routes below are site reports whose SQL reads the site,
// environment and query string and nothing about the caller; the caller's right
// to the site is checked before joining (resolveSite applies the same workspace
// rule the handler does). Segments are excluded, because a private segment is
// visible to its owner only, and so are the individual-level lookups — the
// visitor list, search and timeline — which are audited per view.
var sharedReadRoutes = map[string]bool{
	"overview":             true,
	"visitor-insights":     true,
	"insights":             true,
	"anomalies":            true,
	"attribution":          true,
	"events":               true,
	"pages":                true,
	"ecommerce":            true,
	"experience":           true,
	"cohort":               true,
	"adoption":             true,
	"workspace-rollup":     true,
	"feature-intelligence": true,
	"search-analytics":     true,
	"frustration":          true,
	"ai-analytics":         true,
	"path":                 true,
}

// shareableQuery reports whether the query string may be shared between callers:
// anything naming a segment is resolved against the caller's own visibility.
func shareableQuery(r *http.Request) bool {
	for key := range r.URL.Query() {
		if strings.Contains(strings.ToLower(key), "segment") {
			return false
		}
	}
	return true
}

type flightGroup struct {
	mu    sync.Mutex
	calls map[string]*flight
}

type flight struct {
	done    chan struct{}
	waiters int
	// Set before done is closed and never written after.
	ok     bool
	status int
	header http.Header
	body   []byte
}

// do runs serve for the first request with this key and gives the same response
// to every request that arrives with the key while it runs. It reports whether
// the response was shared rather than computed for this request.
//
// A leader whose own request was cancelled — its reader left — has no answer to
// share: its query stopped half-way. Waiting requests then serve themselves
// rather than fail for someone else's closed tab.
func (g *flightGroup) do(ctx context.Context, key string, w http.ResponseWriter, serve func(http.ResponseWriter)) bool {
	g.mu.Lock()
	if g.calls == nil {
		g.calls = map[string]*flight{}
	}
	if call, running := g.calls[key]; running {
		call.waiters++
		g.mu.Unlock()
		select {
		case <-call.done:
		case <-ctx.Done():
			return false
		}
		if !call.ok {
			serve(w)
			return false
		}
		for name, values := range call.header {
			w.Header()[name] = append([]string(nil), values...)
		}
		w.Header().Set("X-Momento-Shared-Read", "1")
		w.WriteHeader(call.status)
		_, _ = w.Write(call.body)
		return true
	}
	call := &flight{done: make(chan struct{})}
	g.calls[key] = call
	g.mu.Unlock()

	recorder := &flightRecorder{header: http.Header{}, status: http.StatusOK}
	defer func() {
		call.ok = ctx.Err() == nil && !recorder.failed
		call.status, call.header, call.body = recorder.status, recorder.header, recorder.body.Bytes()
		g.mu.Lock()
		delete(g.calls, key)
		g.mu.Unlock()
		close(call.done)
	}()
	serve(recorder)
	for name, values := range recorder.header {
		w.Header()[name] = values
	}
	w.WriteHeader(recorder.status)
	_, _ = w.Write(recorder.body.Bytes())
	return false
}

// waiting is how many requests are waiting on the read with this key.
func (g *flightGroup) waiting(key string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	if call, ok := g.calls[key]; ok {
		return call.waiters
	}
	return 0
}

// flightRecorder holds the leader's response so it can be written to the leader
// and copied to the waiters. A handler that panics leaves it marked failed.
type flightRecorder struct {
	header http.Header
	status int
	wrote  bool
	failed bool
	body   bytes.Buffer
}

func (r *flightRecorder) Header() http.Header { return r.header }

func (r *flightRecorder) WriteHeader(status int) {
	if r.wrote {
		return
	}
	r.wrote = true
	r.status = status
}

func (r *flightRecorder) Write(p []byte) (int, error) {
	if !r.wrote {
		r.WriteHeader(http.StatusOK)
	}
	return r.body.Write(p)
}

// shareIdenticalReads wraps one site report. The key is the resolved site, the
// report and the full query string (url.Values.Encode sorts it, so the order
// the parameters were written in does not split a read).
func (s *Server) shareIdenticalReads(report string, next http.HandlerFunc) http.HandlerFunc {
	if !sharedReadRoutes[report] {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !shareableQuery(r) {
			next(w, r)
			return
		}
		siteID, err := s.resolveSite(r, "siteID")
		if err != nil {
			// The handler resolves the site again and answers the refusal itself.
			next(w, r)
			return
		}
		key := siteID.String() + "|" + report + "|" + r.URL.Query().Encode()
		s.sharedReads.do(r.Context(), key, w, func(target http.ResponseWriter) {
			if recorder, ok := target.(*flightRecorder); ok {
				defer func() {
					if recovered := recover(); recovered != nil {
						recorder.failed = true
						panic(recovered)
					}
				}()
			}
			next(target, r)
		})
	}
}
