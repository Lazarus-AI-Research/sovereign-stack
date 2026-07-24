// Package indexes owns versioned pgvector lifecycle. Each version has an
// immutable embedding identity and a provider namespace. Workspace bindings
// point to exactly one active version and carry the maintenance-mode gate used
// while the appliance's single embedding worker is being changed.
package indexes

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

var validStatuses = map[string]bool{
	"building": true, "validating": true, "active": true,
	"inactive": true, "failed": true,
}

func (s *Store) EnsureSchema(ctx context.Context) error {
	statements := []string{
		`CREATE EXTENSION IF NOT EXISTS vector`,
		`CREATE SCHEMA IF NOT EXISTS vectors`,
		`CREATE TABLE IF NOT EXISTS vectors.index_versions (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			workspace_id UUID NOT NULL,
			provider_slug TEXT NOT NULL DEFAULT '',
			profile_id TEXT NOT NULL,
			model_id TEXT NOT NULL,
			model_revision TEXT NOT NULL,
			dimensions INTEGER NOT NULL,
			normalization TEXT NOT NULL,
			distance_metric TEXT NOT NULL,
			query_prefix TEXT NOT NULL DEFAULT '',
			document_prefix TEXT NOT NULL DEFAULT '',
			chunking_strategy TEXT NOT NULL DEFAULT 'recursive-v1',
			preprocessing_version TEXT NOT NULL DEFAULT 'sovereign-embed-v1',
			status TEXT NOT NULL,
			document_count INTEGER NOT NULL DEFAULT 0,
			processed_documents INTEGER NOT NULL DEFAULT 0,
			vector_count BIGINT NOT NULL DEFAULT 0,
			error TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			started_at TIMESTAMPTZ,
			finished_at TIMESTAMPTZ,
			activated_at TIMESTAMPTZ
		)`,
		`ALTER TABLE vectors.index_versions ADD COLUMN IF NOT EXISTS provider_slug TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE vectors.index_versions ADD COLUMN IF NOT EXISTS query_prefix TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE vectors.index_versions ADD COLUMN IF NOT EXISTS document_prefix TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE vectors.index_versions ADD COLUMN IF NOT EXISTS chunking_strategy TEXT NOT NULL DEFAULT 'recursive-v1'`,
		`ALTER TABLE vectors.index_versions ADD COLUMN IF NOT EXISTS preprocessing_version TEXT NOT NULL DEFAULT 'sovereign-embed-v1'`,
		`ALTER TABLE vectors.index_versions ADD COLUMN IF NOT EXISTS document_count INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE vectors.index_versions ADD COLUMN IF NOT EXISTS processed_documents INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE vectors.index_versions ADD COLUMN IF NOT EXISTS vector_count BIGINT NOT NULL DEFAULT 0`,
		`ALTER TABLE vectors.index_versions ADD COLUMN IF NOT EXISTS error TEXT`,
		`ALTER TABLE vectors.index_versions ADD COLUMN IF NOT EXISTS started_at TIMESTAMPTZ`,
		`ALTER TABLE vectors.index_versions ADD COLUMN IF NOT EXISTS finished_at TIMESTAMPTZ`,
		`CREATE TABLE IF NOT EXISTS vectors.workspace_bindings (
			workspace_id UUID PRIMARY KEY,
			provider_slug TEXT NOT NULL UNIQUE,
			active_index_version UUID REFERENCES vectors.index_versions(id),
			maintenance BOOLEAN NOT NULL DEFAULT false,
			maintenance_reason TEXT,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS vectors.embedding_state (
			singleton BOOLEAN PRIMARY KEY DEFAULT true CHECK (singleton),
			profile_id TEXT NOT NULL,
			provider TEXT NOT NULL,
			served_model_name TEXT NOT NULL,
			dimensions INTEGER NOT NULL,
			activated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS index_versions_one_active
			ON vectors.index_versions(workspace_id) WHERE status = 'active'`,
	}
	for _, statement := range statements {
		if _, err := s.pool.Exec(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

type Version struct {
	ID                   string     `json:"id"`
	WorkspaceID          string     `json:"workspace_id"`
	ProviderSlug         string     `json:"provider_slug"`
	ProfileID            string     `json:"profile_id"`
	ModelID              string     `json:"model_id"`
	ModelRevision        string     `json:"model_revision"`
	Dimensions           int        `json:"dimensions"`
	Normalization        string     `json:"normalization"`
	DistanceMetric       string     `json:"distance_metric"`
	QueryPrefix          string     `json:"query_prefix,omitempty"`
	DocumentPrefix       string     `json:"document_prefix,omitempty"`
	ChunkingStrategy     string     `json:"chunking_strategy"`
	PreprocessingVersion string     `json:"preprocessing_version"`
	Status               string     `json:"status"`
	DocumentCount        int        `json:"document_count"`
	ProcessedDocuments   int        `json:"processed_documents"`
	VectorCount          int64      `json:"vector_count"`
	Error                *string    `json:"error,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	StartedAt            *time.Time `json:"started_at,omitempty"`
	FinishedAt           *time.Time `json:"finished_at,omitempty"`
	ActivatedAt          *time.Time `json:"activated_at,omitempty"`
}

type EmbeddingState struct {
	ProfileID       string    `json:"profile_id"`
	Provider        string    `json:"provider"`
	ServedModelName string    `json:"served_model_name"`
	Dimensions      int       `json:"dimensions"`
	ActivatedAt     time.Time `json:"activated_at"`
}

type Binding struct {
	WorkspaceID  string `json:"workspace_id"`
	ProviderSlug string `json:"provider_slug"`
}

const columns = `id, workspace_id, provider_slug, profile_id, model_id, model_revision,
	dimensions, normalization, distance_metric, query_prefix, document_prefix,
	chunking_strategy, preprocessing_version, status, document_count,
	processed_documents, vector_count, error, created_at, started_at, finished_at, activated_at`

func scan(row pgx.Row) (Version, error) {
	var v Version
	err := row.Scan(&v.ID, &v.WorkspaceID, &v.ProviderSlug, &v.ProfileID, &v.ModelID,
		&v.ModelRevision, &v.Dimensions, &v.Normalization, &v.DistanceMetric,
		&v.QueryPrefix, &v.DocumentPrefix, &v.ChunkingStrategy,
		&v.PreprocessingVersion, &v.Status, &v.DocumentCount,
		&v.ProcessedDocuments, &v.VectorCount, &v.Error, &v.CreatedAt,
		&v.StartedAt, &v.FinishedAt, &v.ActivatedAt)
	return v, err
}

type CreateRequest struct {
	WorkspaceID          string `json:"workspace_id"`
	ProviderSlug         string `json:"provider_slug"`
	ProfileID            string `json:"profile_id"`
	ModelID              string `json:"model_id"`
	ModelRevision        string `json:"model_revision"`
	Dimensions           int    `json:"dimensions"`
	Normalization        string `json:"normalization"`
	DistanceMetric       string `json:"distance_metric"`
	QueryPrefix          string `json:"query_prefix,omitempty"`
	DocumentPrefix       string `json:"document_prefix,omitempty"`
	ChunkingStrategy     string `json:"chunking_strategy"`
	PreprocessingVersion string `json:"preprocessing_version"`
}

func (r *CreateRequest) defaults() {
	if r.ModelRevision == "" {
		r.ModelRevision = "unknown"
	}
	if r.Normalization == "" {
		r.Normalization = "l2"
	}
	if r.DistanceMetric == "" {
		r.DistanceMetric = "cosine"
	}
	if r.ChunkingStrategy == "" {
		r.ChunkingStrategy = "recursive-v1"
	}
	if r.PreprocessingVersion == "" {
		r.PreprocessingVersion = "sovereign-embed-v1"
	}
}

func (r CreateRequest) validate(allowPending bool) error {
	if r.WorkspaceID == "" || r.ProviderSlug == "" || r.ProfileID == "" || r.ModelID == "" {
		return errors.New("workspace_id, provider_slug, profile_id, and model_id are required")
	}
	if !allowPending && r.Dimensions < 1 {
		return errors.New("dimensions must be discovered from the loaded runtime")
	}
	return nil
}

func (s *Store) create(ctx context.Context, request CreateRequest, allowPending bool) (Version, error) {
	request.defaults()
	if err := request.validate(allowPending); err != nil {
		return Version{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Version{}, err
	}
	defer tx.Rollback(ctx)
	row := tx.QueryRow(ctx, `
		INSERT INTO vectors.index_versions
			(workspace_id, provider_slug, profile_id, model_id, model_revision,
			 dimensions, normalization, distance_metric, query_prefix,
			 document_prefix, chunking_strategy, preprocessing_version, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'building')
		RETURNING `+columns, request.WorkspaceID, request.ProviderSlug, request.ProfileID,
		request.ModelID, request.ModelRevision, request.Dimensions, request.Normalization,
		request.DistanceMetric, request.QueryPrefix, request.DocumentPrefix,
		request.ChunkingStrategy, request.PreprocessingVersion)
	version, err := scan(row)
	if err != nil {
		return Version{}, err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO vectors.workspace_bindings (workspace_id, provider_slug)
		VALUES ($1, $2)
		ON CONFLICT (workspace_id) DO UPDATE SET provider_slug = EXCLUDED.provider_slug, updated_at = now()`,
		request.WorkspaceID, request.ProviderSlug)
	if err != nil {
		return Version{}, err
	}
	return version, tx.Commit(ctx)
}

func (s *Store) Create(ctx context.Context, request CreateRequest) (Version, error) {
	return s.create(ctx, request, false)
}

func (s *Store) CreatePending(ctx context.Context, request CreateRequest) (Version, error) {
	request.Dimensions = 0
	return s.create(ctx, request, true)
}

func (s *Store) List(ctx context.Context) ([]Version, error) {
	rows, err := s.pool.Query(ctx, "SELECT "+columns+" FROM vectors.index_versions ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	versions := []Version{}
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
	tag, err := s.pool.Exec(ctx, `UPDATE vectors.index_versions SET status=$2,
		started_at=CASE WHEN $2='building' THEN COALESCE(started_at,now()) ELSE started_at END,
		finished_at=CASE WHEN $2 IN ('active','failed') THEN now() ELSE finished_at END
		WHERE id=$1`, id, status)
	if err == nil && tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return err
}

func (s *Store) SetDimensions(ctx context.Context, id string, dimensions int) error {
	if dimensions < 1 {
		return errors.New("dimensions must be positive")
	}
	_, err := s.pool.Exec(ctx, "UPDATE vectors.index_versions SET dimensions=$2 WHERE id=$1", id, dimensions)
	return err
}

func (s *Store) Progress(ctx context.Context, id string, documents, processed int, vectors int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE vectors.index_versions SET document_count=$2,
		processed_documents=$3, vector_count=$4 WHERE id=$1`, id, documents, processed, vectors)
	return err
}

func (s *Store) Fail(ctx context.Context, id string, failure error) error {
	_, err := s.pool.Exec(ctx, `UPDATE vectors.index_versions SET status='failed', error=$2,
		finished_at=now() WHERE id=$1`, id, failure.Error())
	return err
}

func (s *Store) SetMaintenance(ctx context.Context, workspaceID string, enabled bool, reason string) error {
	_, err := s.pool.Exec(ctx, `UPDATE vectors.workspace_bindings SET maintenance=$2,
		maintenance_reason=NULLIF($3,''), updated_at=now() WHERE workspace_id=$1`,
		workspaceID, enabled, reason)
	return err
}

func (s *Store) Activate(ctx context.Context, id string) (Version, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Version{}, err
	}
	defer tx.Rollback(ctx)
	version, err := scan(tx.QueryRow(ctx, "SELECT "+columns+" FROM vectors.index_versions WHERE id=$1 FOR UPDATE", id))
	if err != nil {
		return Version{}, err
	}
	if version.Status != "validating" || version.Dimensions < 1 || (version.VectorCount < 1 && version.DocumentCount > 0) {
		return Version{}, errors.New("only a validated index can be activated")
	}
	if _, err := tx.Exec(ctx, `UPDATE vectors.index_versions SET status='inactive'
		WHERE workspace_id=$1 AND status='active' AND id<>$2`, version.WorkspaceID, id); err != nil {
		return Version{}, err
	}
	version, err = scan(tx.QueryRow(ctx, `UPDATE vectors.index_versions SET status='active',
		activated_at=now(), finished_at=COALESCE(finished_at,now()) WHERE id=$1 RETURNING `+columns, id))
	if err != nil {
		return Version{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE vectors.workspace_bindings SET active_index_version=$2,
		maintenance=false, maintenance_reason=NULL, updated_at=now() WHERE workspace_id=$1`,
		version.WorkspaceID, id); err != nil {
		return Version{}, err
	}
	return version, tx.Commit(ctx)
}

func (s *Store) EmbeddingState(ctx context.Context) (EmbeddingState, error) {
	var state EmbeddingState
	err := s.pool.QueryRow(ctx, `SELECT profile_id,provider,served_model_name,dimensions,activated_at
		FROM vectors.embedding_state WHERE singleton=true`).
		Scan(&state.ProfileID, &state.Provider, &state.ServedModelName, &state.Dimensions, &state.ActivatedAt)
	return state, err
}

func (s *Store) Bindings(ctx context.Context) ([]Binding, error) {
	rows, err := s.pool.Query(ctx, `SELECT workspace_id::text,provider_slug FROM vectors.workspace_bindings ORDER BY provider_slug`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	bindings := []Binding{}
	for rows.Next() {
		var binding Binding
		if err := rows.Scan(&binding.WorkspaceID, &binding.ProviderSlug); err != nil {
			return nil, err
		}
		bindings = append(bindings, binding)
	}
	return bindings, rows.Err()
}

func (s *Store) SetAllMaintenance(ctx context.Context, enabled bool, reason string) error {
	_, err := s.pool.Exec(ctx, `UPDATE vectors.workspace_bindings SET maintenance=$1,
		maintenance_reason=NULLIF($2,''),updated_at=now()`, enabled, reason)
	return err
}

// ActivationLock serializes appliance-wide provider changes across Control
// replicas. The returned release function must always be called.
func (s *Store) ActivationLock(ctx context.Context) (func(), error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(hashtext('sovereign.embedding.activation'))`); err != nil {
		conn.Release()
		return nil, err
	}
	return func() {
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock(hashtext('sovereign.embedding.activation'))`)
		conn.Release()
	}, nil
}

// ActivateBatch switches every workspace binding and the appliance embedding
// state in one database transaction. Existing active indexes remain untouched
// until all candidates have rebuilt and validated.
func (s *Store) ActivateBatch(ctx context.Context, ids []string, state EmbeddingState) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, id := range ids {
		version, err := scan(tx.QueryRow(ctx, "SELECT "+columns+" FROM vectors.index_versions WHERE id=$1 FOR UPDATE", id))
		if err != nil {
			return err
		}
		if version.Status != "validating" || version.Dimensions < 1 || (version.VectorCount < 1 && version.DocumentCount > 0) {
			return fmt.Errorf("index %s is not validated", id)
		}
		if _, err := tx.Exec(ctx, `UPDATE vectors.index_versions SET status='inactive'
			WHERE workspace_id=$1 AND status='active' AND id<>$2`, version.WorkspaceID, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE vectors.index_versions SET status='active',activated_at=now(),
			finished_at=COALESCE(finished_at,now()) WHERE id=$1`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE vectors.workspace_bindings SET active_index_version=$2,
			maintenance=false,maintenance_reason=NULL,updated_at=now() WHERE workspace_id=$1`, version.WorkspaceID, id); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO vectors.embedding_state
		(singleton,profile_id,provider,served_model_name,dimensions,activated_at)
		VALUES (true,$1,$2,$3,$4,now()) ON CONFLICT (singleton) DO UPDATE SET
		profile_id=EXCLUDED.profile_id,provider=EXCLUDED.provider,
		served_model_name=EXCLUDED.served_model_name,dimensions=EXCLUDED.dimensions,activated_at=now()`,
		state.ProfileID, state.Provider, state.ServedModelName, state.Dimensions)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) Active(ctx context.Context, workspaceID string) (Version, error) {
	return scan(s.pool.QueryRow(ctx, "SELECT "+columns+" FROM vectors.index_versions WHERE workspace_id=$1 AND status='active'", workspaceID))
}

func (s *Store) Namespace(id string, slug string) string { return slug + "::" + id }

func (s *Store) CountVectors(ctx context.Context, id, slug string) (int64, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, "SELECT to_regclass('public.anythingllm_vectors') IS NOT NULL").Scan(&exists); err != nil || !exists {
		return 0, err
	}
	var count int64
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM public.anythingllm_vectors WHERE namespace=$1`, s.Namespace(id, slug)).Scan(&count)
	return count, err
}

func (s *Store) Delete(ctx context.Context, id string) error {
	version, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if version.Status == "active" {
		return errors.New("cannot delete the active index version; activate a replacement first")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var exists bool
	if err := tx.QueryRow(ctx, "SELECT to_regclass('public.anythingllm_vectors') IS NOT NULL").Scan(&exists); err != nil {
		return err
	}
	if exists {
		if _, err := tx.Exec(ctx, `DELETE FROM public.anythingllm_vectors WHERE namespace=$1`, s.Namespace(id, version.ProviderSlug)); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, "DELETE FROM vectors.index_versions WHERE id=$1", id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
