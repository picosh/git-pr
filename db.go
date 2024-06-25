package git

import (
	"log/slog"
	"time"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

type User struct {
	ID        int64     `db:"id"`
	Pubkey    string    `db:"pubkey"`
	Name      string    `db:"name"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

// PatchRequest is a database model for patches submitted to a Repo.
type PatchRequest struct {
	ID        int64     `db:"id"`
	UserID    int64     `db:"user_id"`
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
	UserID         int64     `db:"user_id"`
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

// EventLog is a event log for RSS or other notification systems.
type EventLog struct {
	ID             int64     `db:"id"`
	UserID         int64     `db:"user_id"`
	RepoID         string    `db:"repo_id"`
	PatchRequestID int64     `db:"patch_request_id"`
	Event          string    `db:"event"`
	Data           string    `db:"data"`
	CreatedAt      time.Time `db:"created_at"`
}

// DB is the interface for a pico/git database.
type DB struct {
	*sqlx.DB
	logger *slog.Logger
}

var schema = `
CREATE TABLE IF NOT EXISTS app_users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  pubkey TEXT NOT NULL,
  name TEXT NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS patch_requests (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL,
  repo_id TEXT NOT NULL,
  name TEXT NOT NULL,
  text TEXT NOT NULL,
  status TEXT NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL,
  CONSTRAINT pr_user_id_fk
    FOREIGN KEY(user_id) REFERENCES app_users(id)
    ON DELETE CASCADE
    ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS patches (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL,
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
    ON UPDATE CASCADE,
  CONSTRAINT patches_user_id_fk
    FOREIGN KEY(user_id) REFERENCES app_users(id)
    ON DELETE CASCADE
    ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS event_logs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL,
  repo_id TEXT,
  patch_request_id INTEGER,
  event TEXT NOT NULL,
  data TEXT,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT event_logs_pr_id_fk
  FOREIGN KEY(patch_request_id) REFERENCES patch_requests(id)
  ON DELETE CASCADE
  ON UPDATE CASCADE,
  CONSTRAINT event_logs_user_id_fk
    FOREIGN KEY(user_id) REFERENCES app_users(id)
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
