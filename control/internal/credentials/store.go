// Package credentials stores encrypted remote-provider credentials. Secret
// material is encrypted before it reaches PostgreSQL and never returned by
// list/read APIs.
package credentials

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
	aead cipher.AEAD
}

type Metadata struct {
	ID         string    `json:"id"`
	Provider   string    `json:"provider"`
	Label      string    `json:"label"`
	Configured bool      `json:"configured"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type PutRequest struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Label    string `json:"label"`
	Secret   string `json:"secret"`
}

func DecodeKey(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, errors.New("credential vault key is empty")
	}
	if decoded, err := hex.DecodeString(value); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	if decoded, err := base64.StdEncoding.DecodeString(value); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	if decoded, err := base64.RawURLEncoding.DecodeString(value); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	return nil, errors.New("credential vault key must encode exactly 32 bytes")
}

func LoadKey(path, inline string) ([]byte, error) {
	if inline != "" {
		return DecodeKey(inline)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("credential vault key: %w", err)
	}
	return DecodeKey(string(raw))
}

func New(pool *pgxpool.Pool, key []byte) (*Store, error) {
	if len(key) != 32 {
		return nil, errors.New("credential vault key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Store{pool: pool, aead: aead}, nil
}

func validate(request PutRequest) error {
	if request.ID == "" || request.Provider == "" || request.Label == "" || request.Secret == "" {
		return errors.New("id, provider, label, and secret are required")
	}
	if strings.ContainsAny(request.ID, "/\\ ") {
		return errors.New("credential id may not contain spaces or path separators")
	}
	return nil
}

func (s *Store) seal(id, provider, secret string) ([]byte, []byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}
	aad := []byte(id + "\x00" + provider)
	return s.aead.Seal(nil, nonce, []byte(secret), aad), nonce, nil
}

func (s *Store) Put(ctx context.Context, request PutRequest) (Metadata, error) {
	if err := validate(request); err != nil {
		return Metadata{}, err
	}
	ciphertext, nonce, err := s.seal(request.ID, request.Provider, request.Secret)
	if err != nil {
		return Metadata{}, err
	}
	var out Metadata
	err = s.pool.QueryRow(ctx, `
		INSERT INTO provider_credentials (id, provider, label, ciphertext, nonce)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (id) DO UPDATE SET
			provider = EXCLUDED.provider,
			label = EXCLUDED.label,
			ciphertext = EXCLUDED.ciphertext,
			nonce = EXCLUDED.nonce,
			updated_at = now()
		RETURNING id, provider, label, true, created_at, updated_at`,
		request.ID, request.Provider, request.Label, ciphertext, nonce).
		Scan(&out.ID, &out.Provider, &out.Label, &out.Configured, &out.CreatedAt, &out.UpdatedAt)
	return out, err
}

func (s *Store) List(ctx context.Context) ([]Metadata, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, provider, label, true, created_at, updated_at
		FROM provider_credentials ORDER BY provider, label`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Metadata{}
	for rows.Next() {
		var item Metadata
		if err := rows.Scan(&item.ID, &item.Provider, &item.Label, &item.Configured, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) Secret(ctx context.Context, id string) (string, error) {
	var provider string
	var ciphertext, nonce []byte
	err := s.pool.QueryRow(ctx,
		"SELECT provider, ciphertext, nonce FROM provider_credentials WHERE id = $1", id).
		Scan(&provider, &ciphertext, &nonce)
	if err != nil {
		return "", err
	}
	plain, err := s.aead.Open(nil, nonce, ciphertext, []byte(id+"\x00"+provider))
	if err != nil {
		return "", errors.New("credential could not be decrypted")
	}
	return string(plain), nil
}

func (s *Store) Delete(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, "DELETE FROM provider_credentials WHERE id = $1", id)
	if err == nil && tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return err
}
