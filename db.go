package git

import (
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

// User is a db model for users.
type User struct {
	ID        int64     `db:"id"`
	Pubkey    string    `db:"pubkey"`
	Name      string    `db:"name"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

// Acl is a db model for access control.
type Acl struct {
	ID         int64          `db:"id"`
	Pubkey     sql.NullString `db:"pubkey"`
	IpAddress  sql.NullString `db:"ip_address"`
	Permission string         `db:"permission"`
	CreatedAt  time.Time      `db:"created_at"`
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
	// only used for aggregate queries
	LastUpdated string `db:"last_updated"`
}

type Patchset struct {
	ID             int64     `db:"id"`
	UserID         int64     `db:"user_id"`
	PatchRequestID int64     `db:"patch_request_id"`
	Review         bool      `db:"review"`
	CreatedAt      time.Time `db:"created_at"`
}

// Patch is a database model for a single entry in a patchset.
// This usually corresponds to a git commit.
type Patch struct {
	ID            int64          `db:"id"`
	UserID        int64          `db:"user_id"`
	PatchsetID    int64          `db:"patchset_id"`
	AuthorName    string         `db:"author_name"`
	AuthorEmail   string         `db:"author_email"`
	AuthorDate    string         `db:"author_date"`
	Title         string         `db:"title"`
	Body          string         `db:"body"`
	BodyAppendix  string         `db:"body_appendix"`
	CommitSha     string         `db:"commit_sha"`
	ContentSha    string         `db:"content_sha"`
	BaseCommitSha sql.NullString `db:"base_commit_sha"`
	RawText       string         `db:"raw_text"`
	CreatedAt     time.Time      `db:"created_at"`
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
  pubkey TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL UNIQUE,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS acl (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  pubkey string,
  ip_address string,
  permission string NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
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

CREATE TABLE IF NOT EXISTS patchsets (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL,
  patch_request_id INTEGER NOT NULL,
  review BOOLEAN NOT NULL DEFAULT false,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT patchset_user_id_fk
    FOREIGN KEY(user_id) REFERENCES app_users(id)
    ON DELETE CASCADE
    ON UPDATE CASCADE,
  CONSTRAINT patchset_patch_request_id_fk
    FOREIGN KEY(patch_request_id) REFERENCES patch_requests(id)
    ON DELETE CASCADE
    ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS patches (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL,
	patchset_id INTEGER NOT NULL,
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
	base_commit_sha TEXT,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	CONSTRAINT patches_user_id_fk
		FOREIGN KEY(user_id) REFERENCES app_users(id)
		ON DELETE CASCADE
		ON UPDATE CASCADE,
	CONSTRAINT patches_patchset_id_fk
		FOREIGN KEY(patchset_id) REFERENCES patchsets(id)
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

var sqliteMigrations = []string{
	"", // migration #0 is reserved for schema initialization
	"ALTER TABLE patches ADD COLUMN base_commit_sha TEXT",
	`
	CREATE TABLE IF NOT EXISTS patchsets (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		patch_request_id INTEGER NOT NULL,
		review BOOLEAN NOT NULL DEFAULT false,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		CONSTRAINT patchset_user_id_fk
			FOREIGN KEY(user_id) REFERENCES app_users(id)
			ON DELETE CASCADE
			ON UPDATE CASCADE,
		CONSTRAINT patchset_patch_request_id_fk
			FOREIGN KEY(patch_request_id) REFERENCES patch_requests(id)
			ON DELETE CASCADE
			ON UPDATE CASCADE
	);
	ALTER TABLE patches ADD COLUMN patchset_id INTEGER;
	ALTER TABLE ADD CONSTRAINT patches_patchset_id_fk
		FOREIGN KEY(patchset_id) REFERENCES patchset(id)
		ON DELETE CASCADE
		ON UPDATE CASCADE;
	`,
}

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

	err = d.upgrade()
	if err != nil {
		d.Close()
		return nil, err
	}

	return d, nil
}

// Close implements db.DB.
func (d *DB) Close() error {
	return d.DB.Close()
}

type OldPatch struct {
	ID             int64 `db:"id"`
	UserID         int64 `db:"user_id"`
	PatchRequestID int64 `db:"patch_request_id"`
	Review         bool  `db:"review"`
}

func (db *DB) migrateToPatchsets() error {
	tx, err := db.Beginx()
	if err != nil {
		return err
	}

	defer func() {
		_ = tx.Rollback()
	}()

	var patches []*OldPatch
	err = db.Select(&patches, "SELECT id, user_id, patch_request_id, review FROM patches")
	if err != nil {
		return err
	}
	var patchsetID int64
	for _, patch := range patches {
		rev := "0"
		if patch.Review {
			rev = "1"
		}
		var patchset *Patchset
		err := db.Get(
			&patchset,
			"SELECT * FROM patchsets WHERE patch_request_id=? and review='?' LIMIT 1",
			patch.PatchRequestID,
			rev,
		)
		if err != nil {
			continue
		}
		// upsert
		if patchset == nil {
			row := db.QueryRow(
				"INSERT INTO patchsets (user_id, patch_request_id, review) VALUES(?, ?, ?) RETURNING id;",
				patch.UserID,
				patch.PatchRequestID,
				rev,
			)
			err := row.Scan(&patchsetID)
			if err != nil {
				return err
			}
		}

		_, err = db.Exec(
			"UPDATE patches SET patchset_id=? WHERE id=?",
			patchsetID,
			patch.ID,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (db *DB) upgrade() error {
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("failed to query schema version: %v", err)
	}

	if version == len(sqliteMigrations) {
		return nil
	} else if version > len(sqliteMigrations) {
		return fmt.Errorf("git-pr (version %d) older than schema (version %d)", len(sqliteMigrations), version)
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if version == 0 {
		if _, err := tx.Exec(schema); err != nil {
			return fmt.Errorf("failed to initialize schema: %v", err)
		}
	} else {
		for i := version; i < len(sqliteMigrations); i++ {
			if _, err := tx.Exec(sqliteMigrations[i]); err != nil {
				return fmt.Errorf("failed to execute migration #%v: %v", i, err)
			}
		}
		err = db.migrateToPatchsets()
		if err != nil {
			return fmt.Errorf("failed to migrate to patchsets: %v", err)
		}
	}

	// For some reason prepared statements don't work here
	_, err = tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", len(sqliteMigrations)))
	if err != nil {
		return fmt.Errorf("failed to bump schema version: %v", err)
	}

	return tx.Commit()
}
