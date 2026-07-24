// Package jobs is a DB-backed queue for long-running Control operations
// (model loads, index rebuilds, backups, eval runs). Single-node by design:
// claims use FOR UPDATE SKIP LOCKED so restarts and future concurrency are
// safe.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler func(ctx context.Context, payload json.RawMessage) (result any, err error)

type Runner struct {
	pool     *pgxpool.Pool
	handlers map[string]Handler
	interval time.Duration
	stalled  time.Duration
	mu       sync.Mutex
	running  map[string]context.CancelFunc
}

func New(pool *pgxpool.Pool) *Runner {
	return &Runner{
		pool: pool, handlers: map[string]Handler{}, interval: 2 * time.Second, stalled: 45 * time.Minute,
		running: map[string]context.CancelFunc{},
	}
}

func (r *Runner) Register(kind string, handler Handler) {
	r.handlers[kind] = handler
}

type Job struct {
	ID              string          `json:"id"`
	Kind            string          `json:"kind"`
	Status          string          `json:"status"`
	Stage           string          `json:"stage"`
	Message         *string         `json:"message,omitempty"`
	ProgressCurrent int64           `json:"progress_current"`
	ProgressTotal   *int64          `json:"progress_total,omitempty"`
	ProgressUnit    *string         `json:"progress_unit,omitempty"`
	ProgressRate    *int64          `json:"progress_rate,omitempty"`
	ETASeconds      *int64          `json:"eta_seconds,omitempty"`
	Payload         json.RawMessage `json:"payload,omitempty"`
	Result          json.RawMessage `json:"result,omitempty"`
	Error           *string         `json:"error,omitempty"`
	ErrorCode       *string         `json:"error_code,omitempty"`
	Action          *string         `json:"action,omitempty"`
	CancelRequested bool            `json:"cancel_requested"`
	RetryOf         *string         `json:"retry_of,omitempty"`
	InitiatedBy     *int64          `json:"initiated_by,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	StartedAt       *time.Time      `json:"started_at,omitempty"`
	HeartbeatAt     time.Time       `json:"heartbeat_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	FinishedAt      *time.Time      `json:"finished_at,omitempty"`
}

// Progress is a structured update emitted by a long-running operation.
// Total may be nil for indeterminate work. Unit is normally "steps", "bytes",
// "documents", or "items".
type Progress struct {
	Stage   string
	Message string
	Current int64
	Total   *int64
	Unit    string
}

type reporterContextKey struct{}
type initiatorContextKey struct{}

type reporter struct {
	runner *Runner
	jobID  string
}

// Report records progress for the job associated with ctx. It is a no-op for
// handlers invoked outside the Runner, which preserves direct handler tests.
func Report(ctx context.Context, progress Progress) error {
	value, ok := ctx.Value(reporterContextKey{}).(reporter)
	if !ok {
		return nil
	}
	return value.runner.report(ctx, value.jobID, progress)
}

// IsCancellationRequested lets handlers distinguish an operator cancellation
// from other context cancellation when they need custom cleanup.
func IsCancellationRequested(ctx context.Context) bool {
	return errors.Is(ctx.Err(), context.Canceled)
}

// WithInitiator associates an authenticated appliance user with jobs enqueued
// through ctx. Existing callers remain valid and create system-owned jobs.
func WithInitiator(ctx context.Context, userID int64) context.Context {
	if userID <= 0 {
		return ctx
	}
	return context.WithValue(ctx, initiatorContextKey{}, userID)
}

func (r *Runner) Enqueue(ctx context.Context, kind string, payload any) (string, error) {
	return r.enqueue(ctx, kind, payload, nil)
}

func (r *Runner) enqueue(ctx context.Context, kind string, payload any, retryOf *string) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	var id string
	var initiatedBy any
	if userID, ok := ctx.Value(initiatorContextKey{}).(int64); ok && userID > 0 {
		initiatedBy = userID
	}
	err = r.pool.QueryRow(ctx,
		"INSERT INTO jobs (kind, payload, retry_of, initiated_by) VALUES ($1, $2, $3, $4) RETURNING id", kind, raw, retryOf, initiatedBy).Scan(&id)
	return id, err
}

func (r *Runner) Get(ctx context.Context, id string) (*Job, error) {
	var job Job
	err := r.pool.QueryRow(ctx, `
		SELECT id, kind, status, stage, message, progress_current, progress_total,
		       progress_unit, progress_rate, eta_seconds, payload, COALESCE(result, 'null'), error, error_code, action,
		       cancel_requested, retry_of, initiated_by, created_at, started_at, heartbeat_at, updated_at, finished_at
		FROM jobs WHERE id = $1`, id).
		Scan(&job.ID, &job.Kind, &job.Status, &job.Stage, &job.Message,
			&job.ProgressCurrent, &job.ProgressTotal, &job.ProgressUnit, &job.ProgressRate, &job.ETASeconds, &job.Payload,
			&job.Result, &job.Error, &job.ErrorCode, &job.Action, &job.CancelRequested, &job.RetryOf,
			&job.InitiatedBy, &job.CreatedAt, &job.StartedAt, &job.HeartbeatAt, &job.UpdatedAt, &job.FinishedAt)
	if err != nil {
		return nil, err
	}
	return &job, nil
}

// List returns the most recently updated jobs. Payload is retained for API
// compatibility but callers should not render it as a user-facing label.
func (r *Runner) List(ctx context.Context, limit int) ([]Job, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, kind, status, stage, message, progress_current, progress_total,
		       progress_unit, progress_rate, eta_seconds, payload, COALESCE(result, 'null'), error, error_code, action,
		       cancel_requested, retry_of, initiated_by, created_at, started_at, heartbeat_at, updated_at, finished_at
		FROM jobs ORDER BY updated_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Job{}
	for rows.Next() {
		var job Job
		if err := rows.Scan(&job.ID, &job.Kind, &job.Status, &job.Stage, &job.Message,
			&job.ProgressCurrent, &job.ProgressTotal, &job.ProgressUnit, &job.ProgressRate, &job.ETASeconds, &job.Payload,
			&job.Result, &job.Error, &job.ErrorCode, &job.Action, &job.CancelRequested, &job.RetryOf,
			&job.InitiatedBy, &job.CreatedAt, &job.StartedAt, &job.HeartbeatAt, &job.UpdatedAt, &job.FinishedAt); err != nil {
			return nil, err
		}
		items = append(items, job)
	}
	return items, rows.Err()
}

func (r *Runner) report(ctx context.Context, id string, progress Progress) error {
	stage := progress.Stage
	if stage == "" {
		stage = "running"
	}
	var total any
	if progress.Total != nil {
		total = *progress.Total
	}
	var unit any
	if progress.Unit != "" {
		unit = progress.Unit
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE jobs SET stage = $2, message = NULLIF($3, ''), progress_current = $4,
		                progress_total = $5, progress_unit = $6,
		                progress_rate = CASE WHEN $6::TEXT = 'bytes' AND progress_current < $4::BIGINT
		                  AND EXTRACT(EPOCH FROM (now() - heartbeat_at)) > 0
		                  THEN (($4::BIGINT - progress_current) / EXTRACT(EPOCH FROM (now() - heartbeat_at)))::BIGINT
		                  ELSE progress_rate END,
		                eta_seconds = CASE WHEN $6::TEXT = 'bytes' AND $5::BIGINT IS NOT NULL AND $5::BIGINT > $4::BIGINT AND progress_current < $4::BIGINT
		                  AND EXTRACT(EPOCH FROM (now() - heartbeat_at)) > 0
		                  THEN (($5::BIGINT - $4::BIGINT) / (($4::BIGINT - progress_current) / EXTRACT(EPOCH FROM (now() - heartbeat_at))))::BIGINT
		                  ELSE eta_seconds END,
		                heartbeat_at = now(), updated_at = now()
		WHERE id = $1`, id, stage, progress.Message, progress.Current, total, unit)
	return err
}

// Cancel requests cancellation. Queued work is canceled immediately; running
// work receives context cancellation and remains cancel_requested until its
// handler has completed cleanup.
func (r *Runner) Cancel(ctx context.Context, id string) (*Job, error) {
	command, err := r.pool.Exec(ctx, `
		UPDATE jobs SET cancel_requested = true,
		  status = CASE WHEN status = 'queued' THEN 'canceled' ELSE status END,
		  stage = CASE WHEN status = 'queued' THEN 'canceled' ELSE 'canceling' END,
		  message = 'Cancellation requested', updated_at = now(),
		  finished_at = CASE WHEN status = 'queued' THEN now() ELSE finished_at END
		WHERE id = $1 AND status IN ('queued', 'running')`, id)
	if err != nil {
		return nil, err
	}
	if command.RowsAffected() == 0 {
		job, getErr := r.Get(ctx, id)
		if getErr != nil {
			return nil, getErr
		}
		return nil, fmt.Errorf("job cannot be canceled from %s", job.Status)
	}
	r.mu.Lock()
	cancel := r.running[id]
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return r.Get(ctx, id)
}

// Retry creates a new queued copy of a failed or canceled job.
func (r *Runner) Retry(ctx context.Context, id string) (string, error) {
	job, err := r.Get(ctx, id)
	if err != nil {
		return "", err
	}
	if job.Status != "failed" && job.Status != "canceled" {
		return "", fmt.Errorf("job cannot be retried from %s", job.Status)
	}
	var payload any
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return "", err
	}
	return r.enqueue(ctx, job.Kind, payload, &job.ID)
}

// claimNext marks the oldest queued job with a registered handler as running.
func (r *Runner) claimNext(ctx context.Context) (*Job, error) {
	kinds := make([]string, 0, len(r.handlers))
	for kind := range r.handlers {
		kinds = append(kinds, kind)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var job Job
	err = tx.QueryRow(ctx, `
		SELECT id, kind, payload FROM jobs
		WHERE status = 'queued' AND kind = ANY($1)
		ORDER BY created_at
		FOR UPDATE SKIP LOCKED
		LIMIT 1`, kinds).Scan(&job.ID, &job.Kind, &job.Payload)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx,
		"UPDATE jobs SET status = 'running', stage = 'starting', message = 'Starting', started_at = now(), heartbeat_at = now(), updated_at = now() WHERE id = $1", job.ID); err != nil {
		return nil, err
	}
	return &job, tx.Commit(ctx)
}

func (r *Runner) finish(ctx context.Context, id string, result any, jobErr error) {
	if jobErr != nil {
		message := jobErr.Error()
		status, stage, code, action := "failed", "failed", "operation_failed", "Review the details and retry the operation."
		lower := strings.ToLower(message)
		switch {
		case strings.Contains(lower, "no space") || strings.Contains(lower, "disk"):
			code, action = "insufficient_storage", "Free disk space, then retry."
		case strings.Contains(lower, "checksum") || strings.Contains(lower, "sha256"):
			code, action = "integrity_check_failed", "Retry the download. If it repeats, verify the configured artifact."
		case strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline"):
			code, action = "operation_timed_out", "Retry. If it repeats, open System and create a support bundle."
		case strings.Contains(lower, "unreachable") || strings.Contains(lower, "connection refused") || strings.Contains(lower, "not ready"):
			code, action = "dependency_unavailable", "Wait for System readiness, then retry."
		}
		if errors.Is(jobErr, context.Canceled) {
			var recordedStage string
			_ = r.pool.QueryRow(ctx, "SELECT stage FROM jobs WHERE id = $1", id).Scan(&recordedStage)
			if recordedStage == "stalled" {
				status, stage, code, message, action = "failed", "stalled", "stalled", "The operation stopped reporting progress", "Review System status, then retry or create a support bundle."
			} else {
				status, stage, code, message, action = "canceled", "canceled", "canceled", "Canceled", "Retry when you are ready."
			}
		}
		r.pool.Exec(ctx,
			"UPDATE jobs SET status = $2, stage = $3, error = $4, error_code = $5, action = $6, message = $4, finished_at = now(), updated_at = now() WHERE id = $1",
			id, status, stage, message, code, action)
		return
	}
	raw, err := json.Marshal(result)
	if err != nil {
		raw = []byte("null")
	}
	r.pool.Exec(ctx,
		"UPDATE jobs SET status = 'succeeded', stage = 'complete', message = 'Complete', result = $2, progress_current = COALESCE(progress_total, progress_current), heartbeat_at = now(), finished_at = now(), updated_at = now() WHERE id = $1", id, raw)
}

func (r *Runner) watchStalled(ctx context.Context) {
	interval := min(r.stalled/4, 5*time.Minute)
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		rows, err := r.pool.Query(ctx, `
			UPDATE jobs SET stage = 'stalled', message = 'No progress has been reported',
			  error_code = 'stalled', action = 'Review System status, then retry or create a support bundle.',
			  cancel_requested = true
			WHERE status = 'running' AND heartbeat_at < now() - make_interval(secs => $1)
			RETURNING id`, int64(r.stalled.Seconds()))
		if err != nil {
			log.Printf("jobs: stalled-operation check failed: %v", err)
			continue
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if rows.Scan(&id) == nil {
				ids = append(ids, id)
			}
		}
		rows.Close()
		for _, id := range ids {
			r.mu.Lock()
			cancel := r.running[id]
			r.mu.Unlock()
			if cancel != nil {
				cancel()
			}
		}
	}
}

// Run polls for work until ctx is cancelled.
func (r *Runner) Run(ctx context.Context) {
	// A process restart cannot safely resume a handler mid-side-effect. Mark
	// interrupted work as retryable instead of replaying it automatically.
	_, _ = r.pool.Exec(ctx, `
		UPDATE jobs SET status = 'failed', stage = 'interrupted', error_code = 'interrupted',
		  error = 'Operation interrupted by a Control restart', action = 'Retry the operation.',
		message = 'Interrupted; safe to retry', finished_at = now(), updated_at = now()
		WHERE status = 'running'`)
	go r.watchStalled(ctx)
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		for {
			job, err := r.claimNext(ctx)
			if err != nil {
				log.Printf("jobs: claim failed: %v", err)
				break
			}
			if job == nil {
				break
			}
			handler := r.handlers[job.Kind]
			jobCtx, cancel := context.WithCancel(ctx)
			jobCtx = context.WithValue(jobCtx, reporterContextKey{}, reporter{runner: r, jobID: job.ID})
			r.mu.Lock()
			r.running[job.ID] = cancel
			r.mu.Unlock()
			result, jobErr := handler(jobCtx, job.Payload)
			cancel()
			r.mu.Lock()
			delete(r.running, job.ID)
			r.mu.Unlock()
			r.finish(context.Background(), job.ID, result, jobErr)
		}
	}
}
