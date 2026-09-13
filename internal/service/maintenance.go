package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Maintenance struct {
	DB     *pgxpool.Pool
	Logger *slog.Logger
}

type aggregateJob struct {
	ID          uuid.UUID
	SiteID      uuid.UUID
	Environment string
	JobType     string
	From        *time.Time
	To          *time.Time
	Attempts    int
}

func (m Maintenance) Run(ctx context.Context) {
	_, _ = m.DB.Exec(ctx, `UPDATE aggregate_jobs SET status='pending',error='recovered after interrupted worker',started_at=NULL WHERE status='running' AND started_at<now()-interval '30 minutes'`)
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		for index := 0; index < 10; index++ {
			ran, err := m.runNext(ctx)
			if err != nil && ctx.Err() == nil && m.Logger != nil {
				m.Logger.Error("aggregate maintenance failed", "error", err)
			}
			if !ran {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// RunPending processes one queued aggregate job and reports whether it finished
// one. It exists so a test, or an operator draining the queue, does not have to
// wait for the scheduler tick. A job that was put back on the queue to be tried
// again is reported as not run, so a caller draining in a loop stops and waits
// instead of spinning on it.
func (m Maintenance) RunPending(ctx context.Context) (bool, error) { return m.runNext(ctx) }

func (m Maintenance) runNext(ctx context.Context) (bool, error) {
	tx, err := m.DB.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var job aggregateJob
	err = tx.QueryRow(ctx, `SELECT id,site_id,environment,job_type,date_from,date_to,attempts FROM aggregate_jobs WHERE status='pending' ORDER BY created_at LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&job.ID, &job.SiteID, &job.Environment, &job.JobType, &job.From, &job.To, &job.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE aggregate_jobs SET status='running',attempts=attempts+1,started_at=now(),error=NULL WHERE id=$1`, job.ID); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	job.Attempts++

	err = m.execute(ctx, job)
	status, errorText := "success", ""
	if err != nil {
		status, errorText = "failed", err.Error()
		if len(errorText) > 1000 {
			errorText = errorText[:1000]
		}
	}
	if err != nil && retryableRebuildError(err) && job.Attempts < maxRebuildAttempts {
		// The rebuild lost a deadlock or a serialisation check against the event
		// worker, which was writing the same daily rows at the same moment. That is
		// the ordinary state of affairs while a backlog of late events drains — a
		// backfill of seventy days left seventeen rebuilds marked failed for it —
		// and the rows are still there to rebuild from once the worker's batch
		// commits. The job goes back to the queue with the reason recorded; the
		// pass ends here so it is not picked up again before the next tick.
		if _, requeueErr := m.DB.Exec(ctx, `UPDATE aggregate_jobs SET status='pending',error=$2,started_at=NULL WHERE id=$1`, job.ID, errorText); requeueErr != nil {
			return true, requeueErr
		}
		if m.Logger != nil {
			m.Logger.Warn("aggregate maintenance will retry", "job_id", job.ID, "attempt", job.Attempts, "error", err)
		}
		return false, nil
	}
	_, updateErr := m.DB.Exec(ctx, `UPDATE aggregate_jobs SET status=$2,error=nullif($3,''),finished_at=now() WHERE id=$1`, job.ID, status, errorText)
	if err != nil {
		return true, err
	}
	return true, updateErr
}

// maxRebuildAttempts bounds how many times a rebuild that keeps losing to the
// event worker is put back on the queue before it is marked failed. The
// maintenance loop ticks every fifteen seconds, so this is a few minutes of
// patience — longer than any one inbox batch, and short enough that a rebuild
// that cannot get through is reported rather than hidden in the queue.
const maxRebuildAttempts = 10

// retryableRebuildError reports whether the rebuild failed for a reason that
// running it again, after the other transaction has finished, resolves:
// deadlock_detected (40P01) and serialization_failure (40001). Everything else —
// a bad date range, a missing table, a cancelled statement — fails the job.
func retryableRebuildError(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "40P01" || pgErr.Code == "40001"
}

func (m Maintenance) execute(ctx context.Context, job aggregateJob) error {
	tx, err := m.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	switch job.JobType {
	case "full_rebuild":
		err = RebuildSiteDerivedData(ctx, tx, job.SiteID)
	case "late_event", "date_range":
		if job.From == nil || job.To == nil {
			return fmt.Errorf("date range is required for %s", job.JobType)
		}
		err = RebuildEnvironmentDateRange(ctx, tx, job.SiteID, job.Environment, *job.From, *job.To)
	default:
		return fmt.Errorf("unsupported aggregate job type %q", job.JobType)
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
