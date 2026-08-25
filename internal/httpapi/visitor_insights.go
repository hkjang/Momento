package httpapi

import (
	"net/http"

	"github.com/hkjang/Momento/internal/insight"
)

// visitorInsights returns the full visitor insight report for the selected period
// and environment. The same report backs the MCP tool and scheduled delivery.
func (s *Server) visitorInsights(w http.ResponseWriter, r *http.Request) {
	siteID, err := s.resolveSite(r, "siteID")
	if err != nil {
		writeError(w, 404, "UNKNOWN_SITE", "site not found")
		return
	}
	from, to, err := s.dateRange(r, siteID)
	if err != nil {
		writeError(w, 400, "INVALID_RANGE", err.Error())
		return
	}
	timezone, location, err := s.siteTimezone(r.Context(), siteID)
	if err != nil {
		writeError(w, 500, "INVALID_TIMEZONE", err.Error())
		return
	}
	previousFrom, previousTo := previousDateRange(from, to, location)
	ctx, cancel := s.analyticalContext(r)
	defer cancel()
	report, err := insight.New(s.DB).Build(ctx, siteID, requestEnvironment(r), from, to, previousFrom, previousTo)
	if err != nil {
		writeQueryError(w, err)
		return
	}
	report["timezone"] = timezone
	writeJSON(w, 200, report)
}
