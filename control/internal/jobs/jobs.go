// Package jobs is a DB-backed queue for long-running Control operations
// (model loads, index rebuilds, backups, eval runs). Single-node by design:
// claims use FOR UPDATE SKIP LOCKED so restarts and future concurrency are
// safe.
package jobs

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler func(ctx context.Context, payload json.RawMessage) (result any, err error)

type Runner struct {
	pool     *pgxpool.Pool
	handlers map[string]Handler
	interval time.Duration
}

func New(pool *pgxpool.Pool) *Runner {
	return &Runner{pool: pool, handlers: map[string]Handler{}, interval: 2 * time.Second}
}

func (r *Runner) Register(kind string, handler Handler) {
	r.handlers[kind] = handler
}

type Job struct {
	ID         string          `json:"id"`
	Kind       string          `json:"kind"`
	Status     string          `json:"status"`
	Payload    json.RawMessage `json:"payload"`
	Result     json.RawMessage `json:"result,omitempty"`
	Error      *string         `json:"error,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
	FinishedAt *time.Time      `json:"finished_at,omitempty"`
}

func (r *Runner) Enqueue(ctx context.Context, kind string, payload any) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	var id string
	err = r.pool.QueryRow(ctx,
		"INSERT INTO jobs (kind, payload) VALUES ($1, $2) RETURNING id", kind, raw).Scan(&id)
	return id, err
}

func (r *Runner) Get(ctx context.Context, id string) (*Job, error) {
	var job Job
	err := r.pool.QueryRow(ctx, `
		SELECT id, kind, status, payload, COALESCE(result, 'null'), error, created_at, finished_at
		FROM jobs WHERE id = $1`, id).
		Scan(&job.ID, &job.Kind, &job.Status, &job.Payload, &job.Result, &job.Error, &job.CreatedAt, &job.FinishedAt)
	if err != nil {
		return nil, err
	}
	return &job, nil
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
		"UPDATE jobs SET status = 'running', started_at = now() WHERE id = $1", job.ID); err != nil {
		return nil, err
	}
	return &job, tx.Commit(ctx)
}

func (r *Runner) finish(ctx context.Context, id string, result any, jobErr error) {
	if jobErr != nil {
		message := jobErr.Error()
		r.pool.Exec(ctx,
			"UPDATE jobs SET status = 'failed', error = $2, finished_at = now() WHERE id = $1", id, message)
		return
	}
	raw, err := json.Marshal(result)
	if err != nil {
		raw = []byte("null")
	}
	r.pool.Exec(ctx,
		"UPDATE jobs SET status = 'succeeded', result = $2, finished_at = now() WHERE id = $1", id, raw)
}

// Run polls for work until ctx is cancelled.
func (r *Runner) Run(ctx context.Context) {
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
			result, jobErr := handler(ctx, job.Payload)
			r.finish(ctx, job.ID, result, jobErr)
		}
	}
}
