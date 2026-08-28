package httpapi

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/hkjang/Momento/internal/insight"
)

// The session read moved to internal/insight so the scheduled digest can report
// the same period the overview screen does. These names keep the call sites
// reading the way they did.
type sessionMetrics = insight.SessionMetrics

func (s *Server) readSessionMetrics(ctx context.Context, siteID uuid.UUID, environment string, from, to time.Time) (sessionMetrics, error) {
	return insight.New(s.DB).SessionMetrics(ctx, siteID, environment, from, to)
}
