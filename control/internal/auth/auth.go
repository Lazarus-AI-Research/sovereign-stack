// Package auth: single-admin authentication for Sovereign Control.
// argon2id password hashes, opaque session tokens stored hashed.
// Enterprise authentication is post-MVP (design.md §27).
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
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

// Login verifies credentials and returns an opaque session token.
func (s *Service) Login(ctx context.Context, username, password string) (string, error) {
	var userID int64
	var hash string
	err := s.pool.QueryRow(ctx,
		"SELECT id, password_hash FROM admin_users WHERE username = $1", username).Scan(&userID, &hash)
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
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	if _, err := s.pool.Exec(ctx,
		"INSERT INTO sessions (token_hash, user_id, expires_at) VALUES ($1, $2, $3)",
		hashToken(token), userID, time.Now().Add(SessionTTL)); err != nil {
		return "", err
	}
	return token, nil
}

// Validate returns the username for a live session token.
func (s *Service) Validate(ctx context.Context, token string) (string, error) {
	var username string
	err := s.pool.QueryRow(ctx, `
		SELECT u.username FROM sessions s
		JOIN admin_users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.expires_at > now()`, hashToken(token)).Scan(&username)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrInvalidCredentials
	}
	return username, err
}

func (s *Service) Logout(ctx context.Context, token string) error {
	_, err := s.pool.Exec(ctx, "DELETE FROM sessions WHERE token_hash = $1", hashToken(token))
	return err
}
