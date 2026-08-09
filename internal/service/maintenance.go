package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

func (m Maintenance) runNext(ctx context.Context) (bool, error) {
	tx, err := m.DB.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var job aggregateJob
	err = tx.QueryRow(ctx, `SELECT id,site_id,environment,job_type,date_from,date_to FROM aggregate_jobs WHERE status='pending' ORDER BY created_at LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&job.ID, &job.SiteID, &job.Environment, &job.JobType, &job.From, &job.To)
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

	err = m.execute(ctx, job)
	status, errorText := "success", ""
	if err != nil {
		status, errorText = "failed", err.Error()
		if len(errorText) > 1000 {
			errorText = errorText[:1000]
		}
	}
	_, updateErr := m.DB.Exec(ctx, `UPDATE aggregate_jobs SET status=$2,error=nullif($3,''),finished_at=now() WHERE id=$1`, job.ID, status, errorText)
	if err != nil {
		return true, err
	}
	return true, updateErr
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
