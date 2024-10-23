package git

import (
	"fmt"
	"log/slog"

	"github.com/jmoiron/sqlx"
)

var sqliteSchema = `
CREATE TABLE IF NOT EXISTS app_users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  pubkey TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL UNIQUE,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS repos (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL,
  name TEXT NOT NULL UNIQUE,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT repo_user_id_fk
      FOREIGN KEY(user_id) REFERENCES app_users(id)
      ON DELETE CASCADE
      ON UPDATE CASCADE
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
	patchset_id INTEGER,
	event TEXT NOT NULL,
	data TEXT,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	CONSTRAINT event_logs_pr_id_fk
		FOREIGN KEY(patch_request_id) REFERENCES patch_requests(id)
		ON DELETE CASCADE
		ON UPDATE CASCADE,
	CONSTRAINT event_logs_patchset_id_fk
		FOREIGN KEY(patchset_id) REFERENCES patchsets(id)
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
	// added this by accident
	"",
	// create repos table
	`CREATE TABLE IF NOT EXISTS repos (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE (user_id, name),
		CONSTRAINT repo_user_id_fk
			FOREIGN KEY(user_id) REFERENCES app_users(id)
			ON DELETE CASCADE
			ON UPDATE CASCADE
	);`,
	// migrate existing repo info from patch_requests
	`INSERT INTO repos (user_id, name) SELECT user_id, repo_id from patch_requests group by repo_id;`,
	// convert patch_requests.repo_id to integer with FK constraint
	`CREATE TABLE IF NOT EXISTS tmp_patch_requests (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		repo_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		text TEXT NOT NULL,
		status TEXT NOT NULL,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL,
		CONSTRAINT pr_user_id_fk
			FOREIGN KEY(user_id) REFERENCES app_users(id)
			ON DELETE CASCADE
			ON UPDATE CASCADE,
		CONSTRAINT pr_repo_id_fk
			FOREIGN KEY(repo_id) REFERENCES repos(id)
			ON DELETE CASCADE
			ON UPDATE CASCADE
	);
	INSERT INTO tmp_patch_requests (user_id, repo_id, name, text, status, created_at, updated_at)
		SELECT pr.user_id, repos.id, pr.name, pr.text, pr.status, pr.created_at, pr.updated_at
		FROM patch_requests AS pr
		INNER JOIN repos ON repos.name = pr.repo_id;
	DROP TABLE patch_requests;
	ALTER TABLE tmp_patch_requests RENAME TO patch_requests;`,
	// convert event_logs.repo_id to integer with FK constraint
	`CREATE TABLE IF NOT EXISTS tmp_event_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		repo_id INTEGER,
		patch_request_id INTEGER,
		patchset_id INTEGER,
		event TEXT NOT NULL,
		data TEXT,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		CONSTRAINT event_logs_pr_id_fk
			FOREIGN KEY(patch_request_id) REFERENCES patch_requests(id)
			ON DELETE CASCADE
			ON UPDATE CASCADE,
		CONSTRAINT event_logs_patchset_id_fk
			FOREIGN KEY(patchset_id) REFERENCES patchsets(id)
			ON DELETE CASCADE
			ON UPDATE CASCADE,
		CONSTRAINT event_logs_user_id_fk
			FOREIGN KEY(user_id) REFERENCES app_users(id)
			ON DELETE CASCADE
			ON UPDATE CASCADE
		CONSTRAINT event_logs_repo_id_fk
			FOREIGN KEY(repo_id) REFERENCES repos(id)
			ON DELETE CASCADE
			ON UPDATE CASCADE
	);
	INSERT INTO tmp_event_logs (user_id, repo_id, patch_request_id, patchset_id, event, data, created_at)
		SELECT ev.user_id, repos.id, ev.patch_request_id, ev.patchset_id, ev.event, ev.data, ev.created_at
		FROM event_logs AS ev
		LEFT JOIN repos ON repos.name = ev.repo_id;
	DROP TABLE event_logs;
	ALTER TABLE tmp_event_logs RENAME TO event_logs;`,
}

// Open opens a database connection.
func SqliteOpen(dsn string, logger *slog.Logger) (*sqlx.DB, error) {
	logger.Info("opening db file", "dsn", dsn)
	db, err := sqlx.Connect("sqlite", dsn)
	if err != nil {
		return nil, err
	}

	err = sqliteUpgrade(db)
	if err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

func sqliteUpgrade(db *sqlx.DB) error {
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("failed to query schema version: %v", err)
	}

	if version == len(sqliteMigrations) {
		return nil
	} else if version > len(sqliteMigrations) {
		return fmt.Errorf("git-pr (version %d) older than schema (version %d)", len(sqliteMigrations), version)
	}

	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if version == 0 {
		if _, err := tx.Exec(sqliteSchema); err != nil {
			return fmt.Errorf("failed to initialize schema: %v", err)
		}
	} else {
		for i := version; i < len(sqliteMigrations); i++ {
			if _, err := tx.Exec(sqliteMigrations[i]); err != nil {
				return fmt.Errorf("failed to execute migration #%v: %v", i, err)
			}
		}
	}

	// For some reason prepared statements don't work here
	_, err = tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", len(sqliteMigrations)))
	if err != nil {
		return fmt.Errorf("failed to bump schema version: %v", err)
	}

	return tx.Commit()
}
