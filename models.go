package git

import (
	"time"
)

// PatchRequest is a database model for patches submitted to a Repo
type PatchRequest struct {
	ID        int64     `db:"id"`
	Pubkey    string    `db:"pubkey"`
	RepoID    int64     `db:"repo_id"`
	Name      string    `db:"name"`
	Text      string    `db:"text"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

// Patch is a database model for a single entry in a patchset
// This usually corresponds to a git commit.
type Patch struct {
	ID             int64     `db:"id"`
	Pubkey         string    `db:"pubkey"`
	PatchRequestID int64     `db:"patch_request_id"`
	AuthorName     string    `db:"author_name"`
	AuthorEmail    string    `db:"author_email"`
	Title          string    `db:"title"`
	Body           string    `db:"body"`
	CommitSha      string    `db:"commit_sha"`
	CommitDate     time.Time `db:"commit_date"`
	RawText        string    `db:"raw_text"`
	CreatedAt      time.Time `db:"created_at"`
}

// Comment is a database model for a non-patch comment within a PatchRequest
type Comment struct {
	ID             int64     `db:"id"`
	UserID         int64     `db:"user_id"`
	PatchRequestID int64     `db:"patch_request_id"`
	Text           string    `db:"text"`
	CreatedAt      time.Time `db:"created_at"`
	UpdatedAt      time.Time `db:"updated_at"`
}

type GitDB interface {
}
