package git

import (
	"log/slog"
	"time"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

// PatchRequest is a database model for patches submitted to a Repo.
type PatchRequest struct {
	ID        int64     `db:"id"`
	Pubkey    string    `db:"pubkey"`
	RepoID    string    `db:"repo_id"`
	Name      string    `db:"name"`
	Text      string    `db:"text"`
	Status    string    `db:"status"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

// Patch is a database model for a single entry in a patchset.
// This usually corresponds to a git commit.
type Patch struct {
	ID             int64     `db:"id"`
	Pubkey         string    `db:"pubkey"`
	PatchRequestID int64     `db:"patch_request_id"`
	AuthorName     string    `db:"author_name"`
	AuthorEmail    string    `db:"author_email"`
	AuthorDate     string    `db:"author_date"`
	Title          string    `db:"title"`
	Body           string    `db:"body"`
	BodyAppendix   string    `db:"body_appendix"`
	CommitSha      string    `db:"commit_sha"`
	ContentSha     string    `db:"content_sha"`
	Review         bool      `db:"review"`
	RawText        string    `db:"raw_text"`
	CreatedAt      time.Time `db:"created_at"`
}

// Comment is a database model for a non-patch comment within a PatchRequest.
type Comment struct {
	ID             int64     `db:"id"`
	Pubkey         string    `db:"pubkey"`
	PatchRequestID int64     `db:"patch_request_id"`
	Text           string    `db:"text"`
	CreatedAt      time.Time `db:"created_at"`
	UpdatedAt      time.Time `db:"updated_at"`
}

type GitDB interface {
}

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
  status TEXT NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS patches (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  pubkey TEXT NOT NULL,
  patch_request_id INTEGER NOT NULL,
  author_name TEXT NOT NULL,
  author_email TEXT NOT NULL,
  author_date DATETIME NOT NULL,
  title TEXT NOT NULL,
  body TEXT NOT NULL,
  body_appendix TEXT NOT NULL,
  commit_sha TEXT NOT NULL,
  content_sha TEXT NOT NULL,
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
