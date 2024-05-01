package git

import (
	"database/sql"
	"time"
)

// User is the entity repesenting a pubkey authenticated user
// A user and a single ssh key-pair are synonymous in this context
type User struct {
	ID        int64     `db:"id"`
	Name      string    `db:"name"`
	Pubkey    string    `db:"pubkey"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

// PatchRequest is a database model for patches submitted to a Repo
type PatchRequest struct {
	ID        int64     `db:"id"`
	UserID    int64     `db:"user_id"`
	RepoID    int64     `db:"repo_id"`
	Name      string    `db:"name"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

// Patch is a database model for a single entry in a patchset
// This usually corresponds to a git commit.
type Patch struct {
	ID             int64     `db:"id"`
	UserID         int64     `db:"user_id"`
	PatchRequestID int64     `db:"patch_request_id"`
	FromName       string    `db:"from_name"`
	FromEmail      string    `db:"from_email"`
	Subject        string    `db:"subject"`
	Text           string    `db:"text"`
	Date           time.Time `db:"date"`
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

// Repo is a database model for a repository.
type Repo struct {
	ID          int64         `db:"id"`
	Name        string        `db:"name"`
	ProjectName string        `db:"project_name"`
	Description string        `db:"description"`
	Private     bool          `db:"private"`
	UserID      sql.NullInt64 `db:"user_id"`
	CreatedAt   time.Time     `db:"created_at"`
	UpdatedAt   time.Time     `db:"updated_at"`
}

type GitDB interface {
}
