// Package sqlite implements the storage.Store contract on top of
// modernc.org/sqlite using WAL, a single serialized connection, and
// BEGIN IMMEDIATE write transactions.
package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"

	"github.com/demonidze1337/mia-lead-radar/internal/storage"
)

const driverName = "sqlite"

// pragmas are applied to every connection through the driver DSN.
var pragmas = []string{
	"journal_mode(WAL)",
	"busy_timeout(5000)",
	"foreign_keys(ON)",
	"synchronous(NORMAL)",
}

// SQLiteStore implements storage.Store. A transaction-bound instance carries
// inTx=true and executes every statement through the owning *sql.Conn.
type SQLiteStore struct {
	db        *sql.DB
	exec      dbExecutor
	inTx      bool
	closed    atomic.Bool
	migrateMu sync.Mutex
}

// dbExecutor is implemented by both *sql.DB and *sql.Conn, letting every Store
// method run unchanged inside an active transaction.
type dbExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// New opens (and pings) a SQLite database at dbPath with WAL and safe pragmas.
// The pool is capped at one connection so writers never contend for SQLITE_BUSY.
func New(dbPath string) (*SQLiteStore, error) {
	if strings.TrimSpace(dbPath) == "" {
		return nil, fmt.Errorf("sqlite: empty database path")
	}
	dsn, err := buildDSN(dbPath)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open %s: %w", dbPath, err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	db.SetConnMaxIdleTime(0)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite: ping %s: %w", dbPath, err)
	}
	return &SQLiteStore{db: db, exec: db}, nil
}

func buildDSN(dbPath string) (string, error) {
	query := url.Values{}
	for _, pragma := range pragmas {
		query.Add("_pragma", pragma)
	}
	if dbPath == ":memory:" {
		return "file::memory:?cache=shared&" + query.Encode(), nil
	}
	abs, err := filepath.Abs(dbPath)
	if err != nil {
		return "", fmt.Errorf("sqlite: resolve %s: %w", dbPath, err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
		return "", fmt.Errorf("sqlite: create database directory: %w", err)
	}
	u := url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(abs)}
	u.RawQuery = query.Encode()
	return u.String(), nil
}

// Close releases the underlying database handle. Calls on transaction handles
// are no-ops so a callback cannot close the shared pool.
func (s *SQLiteStore) Close() error {
	if s.inTx || s.db == nil {
		return nil
	}
	if s.closed.Swap(true) {
		return nil
	}
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("sqlite: close: %w", err)
	}
	return nil
}

// WithTx runs fn in a BEGIN IMMEDIATE transaction. fn receives a Store bound to
// the transaction, so nested Store calls share the same atomic unit. Calling
// WithTx on a transaction-bound Store joins the existing transaction.
func (s *SQLiteStore) WithTx(ctx context.Context, fn func(tx storage.Store) error) (err error) {
	if fn == nil {
		return fmt.Errorf("sqlite: nil transaction callback")
	}
	if s.inTx {
		return fn(s)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("sqlite: acquire connection: %w", err)
	}
	defer conn.Close()

	if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("sqlite: begin immediate: %w", err)
	}
	txStore := &SQLiteStore{db: s.db, exec: conn, inTx: true}
	committed := false
	defer func() {
		if committed {
			return
		}
		// Roll back even when the caller's context was canceled mid-transaction.
		if _, rbErr := conn.ExecContext(context.WithoutCancel(ctx), "ROLLBACK"); rbErr != nil {
			if err == nil {
				err = fmt.Errorf("sqlite: rollback: %w", rbErr)
			}
		}
	}()

	if err = fn(txStore); err != nil {
		return err
	}
	if _, err = conn.ExecContext(context.WithoutCancel(ctx), "COMMIT"); err != nil {
		return fmt.Errorf("sqlite: commit: %w", err)
	}
	committed = true
	return nil
}

type migration struct {
	version int
	name    string
}

// Migrate applies embedded migrations in ascending version order inside a
// transaction per migration, recording each version idempotently.
func (s *SQLiteStore) Migrate(ctx context.Context) error {
	if s.inTx {
		return fmt.Errorf("sqlite: migrate cannot run inside a transaction")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.migrateMu.Lock()
	defer s.migrateMu.Unlock()

	// schema_migrations is created by 001, but the runner needs it first.
	if _, err := s.exec.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("sqlite: ensure schema_migrations: %w", err)
	}

	migrations, err := loadMigrations(storage.MigrationsFS)
	if err != nil {
		return err
	}
	applied, err := s.appliedMigrations(ctx)
	if err != nil {
		return err
	}
	for _, m := range migrations {
		if applied[m.version] {
			continue
		}
		body, err := fs.ReadFile(storage.MigrationsFS, m.name)
		if err != nil {
			return fmt.Errorf("sqlite: read migration %s: %w", m.name, err)
		}
		err = s.WithTx(ctx, func(tx storage.Store) error {
			bound := tx.(*SQLiteStore)
			var count int
			if err := bound.exec.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version = ?", m.version).Scan(&count); err != nil {
				return fmt.Errorf("sqlite: check migration %d: %w", m.version, err)
			}
			if count > 0 {
				return nil
			}
			if _, err := bound.exec.ExecContext(ctx, string(body)); err != nil {
				return fmt.Errorf("sqlite: apply migration %s: %w", m.name, err)
			}
			if _, err := bound.exec.ExecContext(ctx,
				"INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(?, ?)",
				m.version, storage.FormatTime(time.Now())); err != nil {
				return fmt.Errorf("sqlite: record migration %d: %w", m.version, err)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) appliedMigrations(ctx context.Context) (map[int]bool, error) {
	rows, err := s.exec.QueryContext(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("sqlite: list migrations: %w", err)
	}
	defer rows.Close()
	applied := make(map[int]bool)
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("sqlite: scan migration version: %w", err)
		}
		applied[version] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: iterate migrations: %w", err)
	}
	return applied, nil
}

func loadMigrations(fsys embed.FS) ([]migration, error) {
	names, err := fs.Glob(fsys, "migrations/*.sql")
	if err != nil {
		return nil, fmt.Errorf("sqlite: glob migrations: %w", err)
	}
	out := make([]migration, 0, len(names))
	for _, name := range names {
		base := filepath.Base(name)
		versionPart, _, ok := strings.Cut(base, "_")
		if !ok {
			return nil, fmt.Errorf("sqlite: migration %q must be named <version>_<name>.sql", base)
		}
		version, err := strconv.Atoi(versionPart)
		if err != nil || version < 1 {
			return nil, fmt.Errorf("sqlite: migration %q has invalid version", base)
		}
		out = append(out, migration{version: version, name: name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}
