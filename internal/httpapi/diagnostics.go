package httpapi

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

type diagnosticCheck struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"` // ok, warn, fail, info
	Detail string `json:"detail"`
	Action string `json:"action,omitempty"`
}

// installDiagnostics answers "why is nothing arriving?" in one request. The most
// common cause is the measured application blocking the collector with its own
// Content-Security-Policy, so the report always carries the exact policy to add.
func (s *Server) installDiagnostics(w http.ResponseWriter, r *http.Request) {
	preventCaching(w)
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	environment := requestEnvironment(r)
	var siteKey, name string
	var domains []string
	var active bool
	var trackingSecret, serverSecret *string
	if s.DB.QueryRow(r.Context(), `SELECT site_key,name,allowed_domains,active,tracking_key_secret,server_api_key_secret FROM sites WHERE id=$1`, siteID).
		Scan(&siteKey, &name, &domains, &active, &trackingSecret, &serverSecret) != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	endpoint := s.publicURL(r.Context(), r)

	var lastEvent *time.Time
	var events24h, events1h, visitors24h, environments int64
	_ = s.DB.QueryRow(r.Context(), `SELECT max(received_at),
		count(*) FILTER(WHERE received_at>now()-interval '24 hours'),
		count(*) FILTER(WHERE received_at>now()-interval '1 hour'),
		count(DISTINCT visitor_id) FILTER(WHERE received_at>now()-interval '24 hours'),
		count(DISTINCT environment) FILTER(WHERE received_at>now()-interval '24 hours')
		FROM raw_events WHERE site_id=$1`, siteID).Scan(&lastEvent, &events24h, &events1h, &visitors24h, &environments)
	var environmentEvents int64
	_ = s.DB.QueryRow(r.Context(), `SELECT count(*) FROM raw_events WHERE site_id=$1 AND environment=$2 AND received_at>now()-interval '24 hours'`, siteID, environment).Scan(&environmentEvents)
	var pending, stalled, deadLetters int64
	_ = s.DB.QueryRow(r.Context(), `SELECT count(*),count(*) FILTER(WHERE created_at<now()-interval '5 minutes') FROM event_inbox WHERE site_id=$1 AND processed_at IS NULL`, siteID).Scan(&pending, &stalled)
	_ = s.DB.QueryRow(r.Context(), `SELECT count(*) FROM event_dead_letters WHERE site_id=$1 AND failed_at>now()-interval '24 hours'`, siteID).Scan(&deadLetters)

	observed := s.observedOrigins(r, siteID)
	unlisted := unlistedOrigins(observed, domains)
	guidance := cspGuidance(endpoint)

	checks := []diagnosticCheck{}
	add := func(check diagnosticCheck) { checks = append(checks, check) }

	if !active {
		add(diagnosticCheck{ID: "site_active", Title: "사이트 상태", Status: "fail", Detail: "사이트가 비활성 상태여서 수집이 거부됩니다.", Action: "관리 → 사이트에서 사이트를 활성화하십시오."})
	} else {
		add(diagnosticCheck{ID: "site_active", Title: "사이트 상태", Status: "ok", Detail: "사이트가 활성 상태입니다."})
	}
	switch {
	case events1h > 0:
		add(diagnosticCheck{ID: "ingestion", Title: "수집 수신", Status: "ok", Detail: fmt.Sprintf("최근 1시간 %d건, 24시간 %d건 수신했습니다.", events1h, events24h)})
	case events24h > 0:
		add(diagnosticCheck{ID: "ingestion", Title: "수집 수신", Status: "warn", Detail: fmt.Sprintf("최근 1시간 수신이 없고 24시간 동안 %d건만 수신했습니다.", events24h), Action: "SDK가 여전히 로드되는지, CSP가 collector를 차단하지 않는지 확인하십시오."})
	default:
		add(diagnosticCheck{ID: "ingestion", Title: "수집 수신", Status: "fail", Detail: "최근 24시간 동안 수집된 이벤트가 없습니다.", Action: "브라우저 Console에 Content-Security-Policy 차단 메시지가 있는지 확인하고, 아래 CSP를 측정 대상 애플리케이션에 적용하십시오."})
	}
	add(diagnosticCheck{
		ID:     "csp",
		Title:  "Content-Security-Policy",
		Status: "info",
		Detail: "측정 대상 애플리케이션의 CSP는 tracker.js 로드(script-src)와 수집 요청(connect-src)에 " + guidance["collector_origin"] + " 을 허용해야 합니다.",
		Action: guidance["header"],
	})
	if len(unlisted) > 0 {
		add(diagnosticCheck{ID: "allowed_domains", Title: "허용 도메인", Status: "warn", Detail: "수집된 이벤트의 도메인 중 허용 목록에 없는 값이 있습니다: " + strings.Join(unlisted, ", "), Action: "관리 → 사이트의 허용 도메인에 추가하십시오. 비어 있으면 모든 도메인을 허용합니다."})
	} else if len(domains) == 0 {
		add(diagnosticCheck{ID: "allowed_domains", Title: "허용 도메인", Status: "warn", Detail: "허용 도메인이 비어 있어 모든 Origin의 수집을 허용합니다.", Action: "운영 환경에서는 서비스 도메인만 허용하십시오."})
	} else {
		add(diagnosticCheck{ID: "allowed_domains", Title: "허용 도메인", Status: "ok", Detail: "수집된 Origin이 모두 허용 목록에 있습니다."})
	}
	if environmentEvents == 0 && events24h > 0 {
		add(diagnosticCheck{ID: "environment", Title: "환경 분리", Status: "warn", Detail: "선택한 " + strings.ToUpper(environment) + " 환경으로 수신된 이벤트가 없습니다.", Action: "SDK의 data-environment 값과 콘솔에서 선택한 환경을 일치시키십시오."})
	} else {
		add(diagnosticCheck{ID: "environment", Title: "환경 분리", Status: "ok", Detail: fmt.Sprintf("최근 24시간 %s 환경에서 %d건 수신했습니다.", strings.ToUpper(environment), environmentEvents)})
	}
	switch {
	case deadLetters > 0:
		add(diagnosticCheck{ID: "pipeline", Title: "적재 파이프라인", Status: "fail", Detail: fmt.Sprintf("최근 24시간 동안 %d건이 Dead Letter로 이동했습니다.", deadLetters), Action: "관리 → Tracking Debugger에서 오류를 확인하십시오."})
	case stalled > 0:
		add(diagnosticCheck{ID: "pipeline", Title: "적재 파이프라인", Status: "warn", Detail: fmt.Sprintf("5분 이상 처리되지 않은 Inbox 항목이 %d건 있습니다.", stalled), Action: "Worker 로그와 데이터베이스 상태를 확인하십시오."})
	default:
		add(diagnosticCheck{ID: "pipeline", Title: "적재 파이프라인", Status: "ok", Detail: fmt.Sprintf("미처리 Inbox %d건, 최근 Dead Letter 0건입니다.", pending)})
	}
	if !s.Secrets.Enabled() {
		add(diagnosticCheck{ID: "secret_storage", Title: "키 영구 저장", Status: "warn", Detail: "MOMENTO_ENCRYPTION_KEY가 없어 발급한 키를 다시 조회할 수 없습니다.", Action: "MOMENTO_ENCRYPTION_KEY를 설정한 뒤 키를 한 번 회전하면 재기동 후에도 조회할 수 있습니다."})
	} else if trackingSecret == nil || serverSecret == nil {
		add(diagnosticCheck{ID: "secret_storage", Title: "키 영구 저장", Status: "warn", Detail: "이 사이트의 키는 암호화 저장 이전에 발급되어 다시 조회할 수 없습니다.", Action: "관리 → 사이트에서 키를 한 번 회전하십시오."})
	} else {
		add(diagnosticCheck{ID: "secret_storage", Title: "키 영구 저장", Status: "ok", Detail: "Tracking Key와 Server API Key가 암호화되어 저장되었고 재기동 후에도 조회할 수 있습니다."})
	}

	writeJSON(w, 200, map[string]any{
		"site_id":            siteKey,
		"name":               name,
		"environment":        environment,
		"collector_endpoint": endpoint,
		"tracking_code":      trackingSnippet(endpoint, siteKey, environment, "full"),
		"csp":                guidance,
		"status":             diagnosticsSummary(checks),
		"checks":             checks,
		"metrics": map[string]any{
			"last_event_at":      lastEvent,
			"events_last_hour":   events1h,
			"events_last_24h":    events24h,
			"visitors_last_24h":  visitors24h,
			"environments_seen":  environments,
			"environment_events": environmentEvents,
			"inbox_pending":      pending,
			"inbox_stalled":      stalled,
			"dead_letters_24h":   deadLetters,
		},
		"observed_origins": observed,
		"unlisted_origins": unlisted,
	})
}

func (s *Server) observedOrigins(r *http.Request, siteID uuid.UUID) []string {
	rows, err := s.DB.Query(r.Context(), `SELECT DISTINCT page_url FROM raw_events WHERE site_id=$1 AND page_url IS NOT NULL AND received_at>now()-interval '24 hours' LIMIT 500`, siteID)
	if err != nil {
		return []string{}
	}
	defer rows.Close()
	seen := map[string]bool{}
	out := []string{}
	for rows.Next() {
		var page *string
		if rows.Scan(&page) != nil || page == nil {
			continue
		}
		parsed, err := url.Parse(*page)
		if err != nil || parsed.Hostname() == "" {
			continue
		}
		host := strings.ToLower(parsed.Hostname())
		if seen[host] {
			continue
		}
		seen[host] = true
		out = append(out, host)
	}
	return out
}

// unlistedOrigins reports observed hosts that the site allowlist does not cover.
func unlistedOrigins(observed, allowed []string) []string {
	if len(allowed) == 0 {
		return []string{}
	}
	out := []string{}
	for _, host := range observed {
		if !hostAllowed(host, allowed) {
			out = append(out, host)
		}
	}
	return out
}

func hostAllowed(host string, allowed []string) bool {
	for _, pattern := range allowed {
		pattern = strings.ToLower(strings.TrimSpace(pattern))
		switch {
		case pattern == "":
			continue
		case strings.HasPrefix(pattern, "*."):
			suffix := pattern[1:]
			if host == pattern[2:] || strings.HasSuffix(host, suffix) {
				return true
			}
		case pattern == host:
			return true
		}
	}
	return false
}

func diagnosticsSummary(checks []diagnosticCheck) string {
	status := "ok"
	for _, check := range checks {
		switch check.Status {
		case "fail":
			return "fail"
		case "warn":
			status = "warn"
		}
	}
	return status
}
