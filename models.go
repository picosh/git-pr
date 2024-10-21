package git

import (
	"database/sql"
	"time"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
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
	AuthorDate    time.Time      `db:"author_date"`
	Title         string         `db:"title"`
	Body          string         `db:"body"`
	BodyAppendix  string         `db:"body_appendix"`
	CommitSha     string         `db:"commit_sha"`
	ContentSha    string         `db:"content_sha"`
	BaseCommitSha sql.NullString `db:"base_commit_sha"`
	RawText       string         `db:"raw_text"`
	CreatedAt     time.Time      `db:"created_at"`
	Files         []*gitdiff.File
}

func (p *Patch) CalcDiff() string {
	return p.RawText
}

// EventLog is a event log for RSS or other notification systems.
type EventLog struct {
	ID             int64         `db:"id"`
	UserID         int64         `db:"user_id"`
	RepoID         string        `db:"repo_id"`
	PatchRequestID sql.NullInt64 `db:"patch_request_id"`
	PatchsetID     sql.NullInt64 `db:"patchset_id"`
	Event          string        `db:"event"`
	Data           string        `db:"data"`
	CreatedAt      time.Time     `db:"created_at"`
}
