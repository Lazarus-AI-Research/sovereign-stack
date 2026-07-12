// Package indexes owns vector index-version lifecycle (design.md §11, §18.7).
// Every vector collection records its embedding identity; changing embedding
// models requires a versioned rebuild, and Control never silently reuses
// vectors from another profile (§11.2).
package indexes

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store runs against the `vectors` logical database (§17), separate from
// sovereign_control.
type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Statuses: building → validating → active | failed; replaced versions
// become inactive.
var validStatuses = map[string]bool{
	"building": true, "validating": true, "active": true, "inactive": true, "failed": true,
}

// EnsureSchema creates the §11.3 table. Idempotent; runs at startup.
func (s *Store) EnsureSchema(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
		CREATE EXTENSION IF NOT EXISTS vector;
		CREATE SCHEMA IF NOT EXISTS vectors;
		CREATE TABLE IF NOT EXISTS vectors.index_versions (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			workspace_id UUID NOT NULL,
			profile_id TEXT NOT NULL,
			model_id TEXT NOT NULL,
			model_revision TEXT NOT NULL,
			dimensions INTEGER NOT NULL,
			normalization TEXT NOT NULL,
			distance_metric TEXT NOT NULL,
			status TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			activated_at TIMESTAMPTZ
		)`)
	return err
}

type Version struct {
	ID             string     `json:"id"`
	WorkspaceID    string     `json:"workspace_id"`
	ProfileID      string     `json:"profile_id"`
	ModelID        string     `json:"model_id"`
	ModelRevision  string     `json:"model_revision"`
	Dimensions     int        `json:"dimensions"`
	Normalization  string     `json:"normalization"`
	DistanceMetric string     `json:"distance_metric"`
	Status         string     `json:"status"`
	CreatedAt      time.Time  `json:"created_at"`
	ActivatedAt    *time.Time `json:"activated_at,omitempty"`
}

const columns = `id, workspace_id, profile_id, model_id, model_revision,
	dimensions, normalization, distance_metric, status, created_at, activated_at`

func scan(row pgx.Row) (Version, error) {
	var v Version
	err := row.Scan(&v.ID, &v.WorkspaceID, &v.ProfileID, &v.ModelID, &v.ModelRevision,
		&v.Dimensions, &v.Normalization, &v.DistanceMetric, &v.Status, &v.CreatedAt, &v.ActivatedAt)
	return v, err
}

type CreateRequest struct {
	WorkspaceID    string `json:"workspace_id"`
	ProfileID      string `json:"profile_id"`
	ModelID        string `json:"model_id"`
	ModelRevision  string `json:"model_revision"`
	Dimensions     int    `json:"dimensions"`
	Normalization  string `json:"normalization"`
	DistanceMetric string `json:"distance_metric"`
}

func (r CreateRequest) validate() error {
	if r.WorkspaceID == "" || r.ProfileID == "" || r.ModelID == "" {
		return fmt.Errorf("workspace_id, profile_id, and model_id are required")
	}
	if r.Dimensions < 1 {
		return fmt.Errorf("dimensions must be a positive integer discovered from the runtime (§10.1)")
	}
	if r.DistanceMetric == "" {
		return fmt.Errorf("distance_metric is required")
	}
	return nil
}

func (s *Store) Create(ctx context.Context, request CreateRequest) (Version, error) {
	if err := request.validate(); err != nil {
		return Version{}, err
	}
	revision := request.ModelRevision
	if revision == "" {
		revision = "unknown"
	}
	normalization := request.Normalization
	if normalization == "" {
		normalization = "l2"
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO vectors.index_versions
			(workspace_id, profile_id, model_id, model_revision, dimensions,
			 normalization, distance_metric, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'building')
		RETURNING `+columns,
		request.WorkspaceID, request.ProfileID, request.ModelID, revision,
		request.Dimensions, normalization, request.DistanceMetric)
	return scan(row)
}

func (s *Store) List(ctx context.Context) ([]Version, error) {
	rows, err := s.pool.Query(ctx, "SELECT "+columns+" FROM vectors.index_versions ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var versions []Version
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		versions = append(versions, v)
	}
	return versions, rows.Err()
}

func (s *Store) Get(ctx context.Context, id string) (Version, error) {
	return scan(s.pool.QueryRow(ctx, "SELECT "+columns+" FROM vectors.index_versions WHERE id = $1", id))
}

func (s *Store) SetStatus(ctx context.Context, id, status string) error {
	if !validStatuses[status] {
		return fmt.Errorf("invalid status %q", status)
	}
	tag, err := s.pool.Exec(ctx, "UPDATE vectors.index_versions SET status = $2 WHERE id = $1", id, status)
	if err == nil && tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return err
}

// Activate atomically switches the workspace's active index (§11.2: "index
// switching should be atomic where practical").
func (s *Store) Activate(ctx context.Context, id string) (Version, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Version{}, err
	}
	defer tx.Rollback(ctx)

	version, err := scan(tx.QueryRow(ctx,
		"SELECT "+columns+" FROM vectors.index_versions WHERE id = $1 FOR UPDATE", id))
	if err != nil {
		return Version{}, err
	}
	if version.Status == "failed" {
		return Version{}, errors.New("cannot activate a failed index version")
	}
	if _, err := tx.Exec(ctx, `
		UPDATE vectors.index_versions SET status = 'inactive'
		WHERE workspace_id = $1 AND status = 'active' AND id <> $2`,
		version.WorkspaceID, id); err != nil {
		return Version{}, err
	}
	version, err = scan(tx.QueryRow(ctx, `
		UPDATE vectors.index_versions SET status = 'active', activated_at = now()
		WHERE id = $1 RETURNING `+columns, id))
	if err != nil {
		return Version{}, err
	}
	return version, tx.Commit(ctx)
}

// Delete removes an index version record. Active versions must be replaced
// first (§11.2: existing vectors remain available until the new index is
// complete).
func (s *Store) Delete(ctx context.Context, id string) error {
	version, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if version.Status == "active" {
		return errors.New("cannot delete the active index version; activate a replacement first")
	}
	_, err = s.pool.Exec(ctx, "DELETE FROM vectors.index_versions WHERE id = $1", id)
	return err
}
