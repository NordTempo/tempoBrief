// Package store owns the SQLite database: opening it, migrations, settings and
// every query. See docs/pipeline.md#data-model.
package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct {
	DB  *sql.DB
	now func() time.Time
}

// Open opens (or creates) the database file and applies pending migrations.
func Open(ctx context.Context, file string) (*Store, error) {
	pragmas := []string{
		"journal_mode(WAL)", "synchronous(NORMAL)", "busy_timeout(5000)",
		"foreign_keys(ON)", "cache_size(-2000)", "auto_vacuum(INCREMENTAL)",
	}
	dsn := "file:" + file + "?_pragma=" + strings.Join(pragmas, "&_pragma=")
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// One writer at a time keeps SQLite simple; readers share the WAL.
	db.SetMaxOpenConns(4)
	s := &Store{DB: db, now: time.Now}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.DB.Close() }

// SetClock replaces the clock, for tests.
func (s *Store) SetClock(now func() time.Time) { s.now = now }

func (s *Store) unix() int64 { return s.now().Unix() }

func (s *Store) migrate(ctx context.Context) error {
	var version int
	if err := s.DB.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, name := range names {
		n, err := strconv.Atoi(strings.SplitN(path.Base(name), "_", 2)[0])
		if err != nil {
			return fmt.Errorf("migration %s: bad name", name)
		}
		if n <= version {
			continue
		}
		body, err := migrations.ReadFile(name)
		if err != nil {
			return err
		}
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", n)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		version = n
	}
	return nil
}

// Ping reports whether the database is writable.
func (s *Store) Ping(ctx context.Context) error {
	return s.SetMeta(ctx, "last_ping", strconv.FormatInt(s.unix(), 10))
}

var ErrNotFound = errors.New("not found")

// Meta reads a value from the meta table: internal state such as when a job last ran.
func (s *Store) Meta(ctx context.Context, key string) (string, error) {
	var v string
	err := s.DB.QueryRowContext(ctx, "SELECT value FROM meta WHERE key = ?", key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return v, err
}

func (s *Store) SetMeta(ctx context.Context, key, value string) error {
	_, err := s.DB.ExecContext(ctx, "INSERT INTO meta(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", key, value)
	return err
}

// MetaTime reads a unix-seconds meta value; zero time when unset.
func (s *Store) MetaTime(ctx context.Context, key string) time.Time {
	v, err := s.Meta(ctx, key)
	if err != nil {
		return time.Time{}
	}
	n, _ := strconv.ParseInt(v, 10, 64)
	return time.Unix(n, 0)
}

func (s *Store) SetMetaTime(ctx context.Context, key string, t time.Time) error {
	return s.SetMeta(ctx, key, strconv.FormatInt(t.Unix(), 10))
}

// Setting decodes a JSON setting into v. Missing keys return ErrNotFound.
func (s *Store) Setting(ctx context.Context, key string, v any) error {
	var raw string
	err := s.DB.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(raw), v)
}

func (s *Store) SetSetting(ctx context.Context, key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, "INSERT INTO settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", key, string(b))
	return err
}

// Vector encoding: float32 little endian, the same layout on every platform.

func encodeVector(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(f))
	}
	return b
}

func decodeVector(b []byte) []float32 {
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return v
}

// nullInt turns a nullable column into a pointer, which JSON renders as null.
func nullInt(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	return &n.Int64
}
