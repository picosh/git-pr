package git

import (
	"log/slog"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite" // sqlite driver
)

// DB is the interface for a pico/git database.
type DB struct {
	*sqlx.DB
	logger *slog.Logger
}

var schema = `
CREATE TABLE IF NOT EXISTS patch_requests (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  pubkey TEXT NOT NULL,
  repo_id TEXT NOT NULL,
  name TEXT NOT NULL,
  text TEXT NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS patches (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  pubkey TEXT NOT NULL,
  patch_request_id INTEGER NOT NULL,
  author_name TEXT NOT NULL,
  author_email TEXT NOT NULL,
  title TEXT NOT NULL,
  body TEXT NOT NULL,
  commit_sha TEXT NOT NULL,
  commit_date DATETIME NOT NULL,
  review BOOLEAN NOT NULL DEFAULT false,
  raw_text TEXT NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT pr_id_fk
  FOREIGN KEY(patch_request_id) REFERENCES patch_requests(id)
  ON DELETE CASCADE
  ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS comments (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  pubkey TEXT NOT NULL,
  patch_request_id INTEGER NOT NULL,
  text TEXT NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL,
  CONSTRAINT pr_id_fk
  FOREIGN KEY(patch_request_id) REFERENCES patch_requests(id)
  ON DELETE CASCADE
  ON UPDATE CASCADE
);
`

// Open opens a database connection.
func Open(dsn string, logger *slog.Logger) (*DB, error) {
	db, err := sqlx.Connect("sqlite", dsn)
	if err != nil {
		return nil, err
	}

	d := &DB{
		DB:     db,
		logger: logger,
	}

	return d, nil
}

func (d *DB) Migrate() {
	// exec the schema or fail; multi-statement Exec behavior varies between
	// database drivers;  pq will exec them all, sqlite3 won't, ymmv
	d.DB.MustExec(schema)
}

// Close implements db.DB.
func (d *DB) Close() error {
	return d.DB.Close()
}
