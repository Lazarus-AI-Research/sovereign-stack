// Package auth implements Sovereign Control identity, role-based access,
// invitations, argon2id password hashes, and opaque hashed session tokens.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/argon2"
)

const (
	argonTime    = 1
	argonMemory  = 64 * 1024 // KiB
	argonThreads = 4
	argonKeyLen  = 32
	saltLen      = 16

	SessionTTL = 24 * time.Hour
)

var ErrInvalidCredentials = errors.New("invalid credentials")
var usernamePattern = regexp.MustCompile(`^[a-z][a-z0-9._@-]{1,63}$`)

const (
	ClaimTTL  = 30 * time.Minute
	InviteTTL = 24 * time.Hour
)

type Identity struct {
	ID           int64    `json:"id"`
	Username     string   `json:"username"`
	DisplayName  string   `json:"display_name"`
	Role         string   `json:"role"`
	Disabled     bool     `json:"disabled"`
	WorkspaceIDs []string `json:"workspace_ids"`
}

type Invitation struct {
	ID           string     `json:"id"`
	Role         string     `json:"role"`
	WorkspaceIDs []string   `json:"workspace_ids"`
	CreatedAt    time.Time  `json:"created_at"`
	ExpiresAt    time.Time  `json:"expires_at"`
	AcceptedAt   *time.Time `json:"accepted_at,omitempty"`
}

// ── argon2id ─────────────────────────────────────────────────────────────

func HashPassword(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf(
		"$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

func VerifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var memory, timeCost uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &timeCost, &threads); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, timeCost, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// ── service ──────────────────────────────────────────────────────────────

type Service struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func randomToken(bytes int) (string, error) {
	raw := make([]byte, bytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func validRole(role string) bool {
	return role == "admin" || role == "manager" || role == "member"
}

// Bootstrap ensures an admin user exists. Password precedence: existing user
// wins; else SOVEREIGN_ADMIN_PASSWORD; else a generated one, logged ONCE.
func (s *Service) Bootstrap(ctx context.Context, username, envPassword string) error {
	var exists bool
	err := s.pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM admin_users)").Scan(&exists)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	password := envPassword
	generated := false
	if password == "" {
		raw := make([]byte, 18)
		if _, err := rand.Read(raw); err != nil {
			return err
		}
		password = base64.RawURLEncoding.EncodeToString(raw)
		generated = true
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx,
		"INSERT INTO admin_users (username, password_hash) VALUES ($1, $2)", username, hash); err != nil {
		return err
	}
	if generated {
		log.Printf("bootstrap: created admin user %q with generated password: %s (change it immediately)", username, password)
	} else {
		log.Printf("bootstrap: created admin user %q from SOVEREIGN_ADMIN_PASSWORD", username)
	}
	return nil
}

// EnsureSetupClaim creates a short-lived, single-use first-admin token when
// the appliance has no users. The caller writes the raw token to an owner-only
// host file; only its hash is stored in Postgres.
func (s *Service) EnsureSetupClaim(ctx context.Context) (string, time.Time, error) {
	var users int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM admin_users").Scan(&users); err != nil {
		return "", time.Time{}, err
	}
	if users > 0 {
		return "", time.Time{}, nil
	}
	token, err := randomToken(32)
	if err != nil {
		return "", time.Time{}, err
	}
	expires := time.Now().Add(ClaimTTL)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", time.Time{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "DELETE FROM setup_claims"); err != nil {
		return "", time.Time{}, err
	}
	if _, err := tx.Exec(ctx, "INSERT INTO setup_claims (token_hash, expires_at) VALUES ($1,$2)", hashToken(token), expires); err != nil {
		return "", time.Time{}, err
	}
	return token, expires, tx.Commit(ctx)
}

func (s *Service) Claim(ctx context.Context, token, username, displayName, password string) (Identity, error) {
	username = strings.ToLower(username)
	if !usernamePattern.MatchString(username) || len(password) < 12 {
		return Identity{}, errors.New("username must be 2-64 lowercase characters starting with a letter, and password must be at least 12 characters")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return Identity{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Identity{}, err
	}
	defer tx.Rollback(ctx)
	var live bool
	if err := tx.QueryRow(ctx, `SELECT expires_at > now() AND consumed_at IS NULL
		FROM setup_claims WHERE token_hash=$1 FOR UPDATE`, hashToken(token)).Scan(&live); err != nil || !live {
		return Identity{}, ErrInvalidCredentials
	}
	var identity Identity
	err = tx.QueryRow(ctx, `INSERT INTO admin_users (username,password_hash,display_name,role)
		VALUES ($1,$2,$3,'admin') RETURNING id,username,display_name,role,disabled`,
		username, hash, displayName).Scan(&identity.ID, &identity.Username, &identity.DisplayName, &identity.Role, &identity.Disabled)
	if err != nil {
		return Identity{}, err
	}
	if _, err := tx.Exec(ctx, "UPDATE setup_claims SET consumed_at=now() WHERE token_hash=$1", hashToken(token)); err != nil {
		return Identity{}, err
	}
	return identity, tx.Commit(ctx)
}

// Login verifies credentials and returns an opaque session token.
func (s *Service) Login(ctx context.Context, username, password string) (string, error) {
	var userID int64
	var hash string
	err := s.pool.QueryRow(ctx,
		"SELECT id, password_hash FROM admin_users WHERE username = $1 AND NOT disabled", strings.ToLower(username)).Scan(&userID, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		// Burn comparable time so absent users are indistinguishable.
		VerifyPassword(password, "$argon2id$v=19$m=65536,t=1,p=4$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
		return "", ErrInvalidCredentials
	}
	if err != nil {
		return "", err
	}
	if !VerifyPassword(password, hash) {
		return "", ErrInvalidCredentials
	}
	return s.IssueSession(ctx, userID)
}

func (s *Service) IssueSession(ctx context.Context, userID int64) (string, error) {
	token, err := randomToken(32)
	if err != nil {
		return "", err
	}
	if _, err := s.pool.Exec(ctx,
		"INSERT INTO sessions (token_hash, user_id, expires_at) VALUES ($1, $2, $3)",
		hashToken(token), userID, time.Now().Add(SessionTTL)); err != nil {
		return "", err
	}
	return token, nil
}

// Validate returns the username for a live session token.
func (s *Service) Validate(ctx context.Context, token string) (string, error) {
	identity, err := s.ValidateIdentity(ctx, token)
	return identity.Username, err
}

func (s *Service) ValidateIdentity(ctx context.Context, token string) (Identity, error) {
	var identity Identity
	err := s.pool.QueryRow(ctx, `SELECT u.id,u.username,u.display_name,u.role,u.disabled
		FROM sessions s JOIN admin_users u ON u.id=s.user_id
		WHERE s.token_hash=$1 AND s.expires_at > now() AND NOT u.disabled`, hashToken(token)).
		Scan(&identity.ID, &identity.Username, &identity.DisplayName, &identity.Role, &identity.Disabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return Identity{}, ErrInvalidCredentials
	}
	if err != nil {
		return Identity{}, err
	}
	rows, err := s.pool.Query(ctx, "SELECT workspace_id FROM workspace_memberships WHERE user_id=$1 ORDER BY workspace_id", identity.ID)
	if err != nil {
		return Identity{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return Identity{}, err
		}
		identity.WorkspaceIDs = append(identity.WorkspaceIDs, id)
	}
	return identity, rows.Err()
}

func (s *Service) ListUsers(ctx context.Context) ([]Identity, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,username,display_name,role,disabled FROM admin_users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []Identity{}
	for rows.Next() {
		var user Identity
		if err := rows.Scan(&user.ID, &user.Username, &user.DisplayName, &user.Role, &user.Disabled); err != nil {
			return nil, err
		}
		membershipRows, err := s.pool.Query(ctx, "SELECT workspace_id FROM workspace_memberships WHERE user_id=$1 ORDER BY workspace_id", user.ID)
		if err != nil {
			return nil, err
		}
		for membershipRows.Next() {
			var id string
			if err := membershipRows.Scan(&id); err != nil {
				membershipRows.Close()
				return nil, err
			}
			user.WorkspaceIDs = append(user.WorkspaceIDs, id)
		}
		membershipRows.Close()
		users = append(users, user)
	}
	return users, rows.Err()
}

func (s *Service) UpdateUser(ctx context.Context, id int64, role string, disabled bool, workspaceIDs []string) (Identity, error) {
	if !validRole(role) {
		return Identity{}, errors.New("role must be admin, manager, or member")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Identity{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext('sovereign.identity.admin-update'))"); err != nil {
		return Identity{}, err
	}
	var user Identity
	err = tx.QueryRow(ctx, `UPDATE admin_users SET role=$2,disabled=$3 WHERE id=$1
		RETURNING id,username,display_name,role,disabled`, id, role, disabled).
		Scan(&user.ID, &user.Username, &user.DisplayName, &user.Role, &user.Disabled)
	if err != nil {
		return Identity{}, err
	}
	var activeAdmins int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM admin_users WHERE role='admin' AND NOT disabled").Scan(&activeAdmins); err != nil {
		return Identity{}, err
	}
	if activeAdmins < 1 {
		return Identity{}, errors.New("at least one active administrator is required")
	}
	if _, err := tx.Exec(ctx, "DELETE FROM workspace_memberships WHERE user_id=$1", id); err != nil {
		return Identity{}, err
	}
	for _, workspaceID := range workspaceIDs {
		if workspaceID == "" {
			continue
		}
		if _, err := tx.Exec(ctx, "INSERT INTO workspace_memberships (user_id,workspace_id) VALUES ($1,$2)", id, workspaceID); err != nil {
			return Identity{}, err
		}
		user.WorkspaceIDs = append(user.WorkspaceIDs, workspaceID)
	}
	return user, tx.Commit(ctx)
}

func (s *Service) CreateInvitation(ctx context.Context, createdBy int64, role string, workspaceIDs []string) (Invitation, string, error) {
	if !validRole(role) {
		return Invitation{}, "", errors.New("role must be admin, manager, or member")
	}
	token, err := randomToken(32)
	if err != nil {
		return Invitation{}, "", err
	}
	expires := time.Now().Add(InviteTTL)
	workspaceJSON, err := json.Marshal(workspaceIDs)
	if err != nil {
		return Invitation{}, "", err
	}
	var invite Invitation
	err = s.pool.QueryRow(ctx, `INSERT INTO invitations (token_hash,role,workspace_ids,created_by,expires_at)
		VALUES ($1,$2,$3::jsonb,$4,$5) RETURNING id::text,role,workspace_ids,created_at,expires_at,accepted_at`,
		hashToken(token), role, string(workspaceJSON), createdBy, expires).
		Scan(&invite.ID, &invite.Role, &invite.WorkspaceIDs, &invite.CreatedAt, &invite.ExpiresAt, &invite.AcceptedAt)
	return invite, token, err
}

func (s *Service) InvitationInfo(ctx context.Context, token string) (Invitation, error) {
	var invite Invitation
	err := s.pool.QueryRow(ctx, `SELECT id::text,role,workspace_ids,created_at,expires_at,accepted_at
		FROM invitations WHERE token_hash=$1 AND expires_at>now() AND accepted_at IS NULL`, hashToken(token)).
		Scan(&invite.ID, &invite.Role, &invite.WorkspaceIDs, &invite.CreatedAt, &invite.ExpiresAt, &invite.AcceptedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Invitation{}, ErrInvalidCredentials
	}
	return invite, err
}

func (s *Service) AcceptInvitation(ctx context.Context, token, username, displayName, password string) (Identity, error) {
	username = strings.ToLower(username)
	if !usernamePattern.MatchString(username) || len(password) < 12 {
		return Identity{}, errors.New("username must be 2-64 lowercase characters starting with a letter, and password must be at least 12 characters")
	}
	passwordHash, err := HashPassword(password)
	if err != nil {
		return Identity{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Identity{}, err
	}
	defer tx.Rollback(ctx)
	var inviteID, role string
	var workspaceIDs []string
	err = tx.QueryRow(ctx, `SELECT id::text,role,workspace_ids FROM invitations
		WHERE token_hash=$1 AND expires_at>now() AND accepted_at IS NULL FOR UPDATE`, hashToken(token)).
		Scan(&inviteID, &role, &workspaceIDs)
	if errors.Is(err, pgx.ErrNoRows) {
		return Identity{}, ErrInvalidCredentials
	}
	if err != nil {
		return Identity{}, err
	}
	var user Identity
	err = tx.QueryRow(ctx, `INSERT INTO admin_users (username,password_hash,display_name,role)
		VALUES ($1,$2,$3,$4) RETURNING id,username,display_name,role,disabled`,
		username, passwordHash, displayName, role).
		Scan(&user.ID, &user.Username, &user.DisplayName, &user.Role, &user.Disabled)
	if err != nil {
		return Identity{}, err
	}
	for _, workspaceID := range workspaceIDs {
		if _, err := tx.Exec(ctx, "INSERT INTO workspace_memberships (user_id,workspace_id) VALUES ($1,$2)", user.ID, workspaceID); err != nil {
			return Identity{}, err
		}
	}
	if _, err := tx.Exec(ctx, "UPDATE invitations SET accepted_by=$2,accepted_at=now() WHERE id=$1::uuid", inviteID, user.ID); err != nil {
		return Identity{}, err
	}
	user.WorkspaceIDs = workspaceIDs
	return user, tx.Commit(ctx)
}

func (s *Service) Logout(ctx context.Context, token string) error {
	_, err := s.pool.Exec(ctx, "DELETE FROM sessions WHERE token_hash = $1", hashToken(token))
	return err
}
