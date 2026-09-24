package accounts

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/plat5dev/operator/internal/apierr"
	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

const tokenTTL = 7 * 24 * time.Hour

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS operators (
			id TEXT PRIMARY KEY,
			email TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			created_at TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS tokens (
			token_hash TEXT PRIMARY KEY,
			operator_id TEXT NOT NULL,
			expires_at TEXT NOT NULL
		);
	`)
	return err
}

func (s *Store) Bootstrap(email, password string) (bool, error) {
	email = normalizeEmail(email)
	if email == "" || password == "" {
		return false, errors.New("bootstrap email and password are required")
	}
	var exists int
	if err := s.db.QueryRow(`SELECT 1 FROM operators WHERE email = ?`, email).Scan(&exists); err == nil {
		return false, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	if _, err := s.create(email, password); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) Create(email, password string) (string, error) {
	email = normalizeEmail(email)
	if email == "" || password == "" {
		return "", errors.New("email and password are required")
	}
	return s.create(email, password)
}

func (s *Store) create(email, password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	id := apierr.NewID()
	_, err = s.db.Exec(
		`INSERT INTO operators (id, email, password_hash, created_at) VALUES (?, ?, ?, ?)`,
		id, email, string(hash), time.Now().UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return "", err
	}
	return id, nil
}

func (s *Store) Login(email, password string) (string, error) {
	_, token, err := s.LoginOperator(email, password)
	return token, err
}

func (s *Store) LoginOperator(email, password string) (string, string, error) {
	email = normalizeEmail(email)
	var id, hash string
	err := s.db.QueryRow(`SELECT id, password_hash FROM operators WHERE email = ?`, email).Scan(&id, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", errUnauthorized
	}
	if err != nil {
		return "", "", err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return "", "", errUnauthorized
	}
	token, err := s.issue(id)
	if err != nil {
		return "", "", err
	}
	return id, token, nil
}

func (s *Store) Authenticate(token string) (string, bool, error) {
	if token == "" {
		return "", false, nil
	}
	var id, exp string
	err := s.db.QueryRow(`SELECT operator_id, expires_at FROM tokens WHERE token_hash = ?`, hashToken(token)).Scan(&id, &exp)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	until, err := time.Parse(time.RFC3339Nano, exp)
	if err != nil || !until.After(time.Now()) {
		_, _ = s.db.Exec(`DELETE FROM tokens WHERE token_hash = ?`, hashToken(token))
		return "", false, nil
	}
	return id, true, nil
}

func (s *Store) issue(operatorID string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	exp := time.Now().UTC().Add(tokenTTL).Format(time.RFC3339Nano)
	_, err := s.db.Exec(
		`INSERT INTO tokens (token_hash, operator_id, expires_at) VALUES (?, ?, ?)`,
		hashToken(token), operatorID, exp,
	)
	if err != nil {
		return "", err
	}
	return token, nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

var errUnauthorized = errors.New("unauthorized")

func IsUnauthorized(err error) bool {
	return errors.Is(err, errUnauthorized)
}
