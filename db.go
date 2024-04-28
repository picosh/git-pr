package git

import "time"

type Patch struct {
	ID        string
	Owner     string
	Contents  string
	CreatedAt *time.Time
}

type GitDB interface {
	InsertPatch(patch *Patch) (*Patch, error)
}
