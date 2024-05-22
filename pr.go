package git

import (
	"bytes"
	"fmt"
	"io"
	"time"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
)

type GitPatchRequest interface {
	GetRepos() ([]Repo, error)
	GetRepoByID(repoID string) (*Repo, error)
	SubmitPatchRequest(pubkey string, repoID string, patches io.Reader) (*PatchRequest, error)
	SubmitPatch(pubkey string, prID int64, review bool, patch io.Reader) (*Patch, error)
	GetPatchRequestByID(prID int64) (*PatchRequest, error)
	GetPatchRequests() ([]*PatchRequest, error)
	GetPatchRequestsByRepoID(repoID string) ([]*PatchRequest, error)
	GetPatchesByPrID(prID int64) ([]*Patch, error)
	UpdatePatchRequest(prID int64, status string) error
}

type PrCmd struct {
	Backend *Backend
}

var _ GitPatchRequest = PrCmd{}
var _ GitPatchRequest = (*PrCmd)(nil)

func (pr PrCmd) GetRepos() ([]Repo, error) {
	return pr.Backend.Cfg.Repos, nil
}

func (pr PrCmd) GetRepoByID(repoID string) (*Repo, error) {
	repos, err := pr.GetRepos()
	if err != nil {
		return nil, err
	}

	for _, repo := range repos {
		if repo.ID == repoID {
			return &repo, nil
		}
	}

	return nil, fmt.Errorf("repo not found: %s", repoID)
}

func (pr PrCmd) GetPatchesByPrID(prID int64) ([]*Patch, error) {
	patches := []*Patch{}
	err := pr.Backend.DB.Select(
		&patches,
		"SELECT * FROM patches WHERE patch_request_id=?",
		prID,
	)
	if err != nil {
		return patches, err
	}
	if len(patches) == 0 {
		return patches, fmt.Errorf("no patches found for Patch Request ID: %d", prID)
	}
	return patches, nil
}

func (cmd PrCmd) GetPatchRequests() ([]*PatchRequest, error) {
	prs := []*PatchRequest{}
	err := cmd.Backend.DB.Select(
		&prs,
		"SELECT * FROM patch_requests",
	)
	return prs, err
}

func (cmd PrCmd) GetPatchRequestsByRepoID(repoID string) ([]*PatchRequest, error) {
	prs := []*PatchRequest{}
	err := cmd.Backend.DB.Select(
		&prs,
		"SELECT * FROM patch_requests WHERE repo_id=?",
		repoID,
	)
	return prs, err
}

func (cmd PrCmd) GetPatchRequestByID(prID int64) (*PatchRequest, error) {
	pr := PatchRequest{}
	err := cmd.Backend.DB.Get(
		&pr,
		"SELECT * FROM patch_requests WHERE id=?",
		prID,
	)
	return &pr, err
}

// Status types: open, close, accept, review.
func (cmd PrCmd) UpdatePatchRequest(prID int64, status string) error {
	_, err := cmd.Backend.DB.Exec(
		"UPDATE patch_requests SET status=? WHERE id=?", status, prID,
	)
	return err
}

func (cmd PrCmd) SubmitPatch(pubkey string, prID int64, review bool, patch io.Reader) (*Patch, error) {
	pr := PatchRequest{}
	err := cmd.Backend.DB.Get(&pr, "SELECT * FROM patch_requests WHERE id=?", prID)
	if err != nil {
		return nil, err
	}

	// need to read io.Reader from session twice
	var buf bytes.Buffer
	tee := io.TeeReader(patch, &buf)

	_, preamble, err := gitdiff.Parse(tee)
	if err != nil {
		return nil, err
	}
	header, err := gitdiff.ParsePatchHeader(preamble)
	if err != nil {
		return nil, err
	}

	patchID := 0
	row := cmd.Backend.DB.QueryRow(
		"INSERT INTO patches (pubkey, patch_request_id, author_name, author_email, author_date, title, body, body_appendix, commit_sha, review, raw_text) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id",
		pubkey,
		prID,
		header.Author.Name,
		header.Author.Email,
		header.AuthorDate,
		header.Title,
		header.Body,
		header.BodyAppendix,
		header.SHA,
		review,
		buf.String(),
	)
	err = row.Scan(&patchID)
	if err != nil {
		return nil, err
	}

	var patchRec Patch
	err = cmd.Backend.DB.Get(&patchRec, "SELECT * FROM patches WHERE id=?", patchID)
	return &patchRec, err
}

func (cmd PrCmd) SubmitPatchRequest(pubkey string, repoID string, patches io.Reader) (*PatchRequest, error) {
	tx, err := cmd.Backend.DB.Beginx()
	if err != nil {
		return nil, err
	}

	defer func() {
		err := tx.Rollback()
		if err != nil {
			cmd.Backend.Logger.Error("rollback", "err", err)
		}
	}()

	// need to read io.Reader from session twice
	var buf bytes.Buffer
	tee := io.TeeReader(patches, &buf)

	_, preamble, err := gitdiff.Parse(tee)
	if err != nil {
		return nil, err
	}
	header, err := gitdiff.ParsePatchHeader(preamble)
	if err != nil {
		return nil, err
	}
	prName := header.Title
	prDesc := header.Body

	var prID int64
	row := tx.QueryRow(
		"INSERT INTO patch_requests (pubkey, repo_id, name, text, status, updated_at) VALUES(?, ?, ?, ?, ?, ?) RETURNING id",
		pubkey,
		repoID,
		prName,
		prDesc,
		"open",
		time.Now(),
	)
	err = row.Scan(&prID)
	if err != nil {
		return nil, err
	}
	if prID == 0 {
		return nil, fmt.Errorf("could not create patch request")
	}

	authorName := ""
	authorEmail := ""
	if header.Author != nil {
		authorName = header.Author.Name
		authorEmail = header.Author.Email
	}

	_, err = tx.Exec(
		"INSERT INTO patches (pubkey, patch_request_id, author_name, author_email, author_date, title, body, body_appendix, commit_sha, raw_text) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		pubkey,
		prID,
		authorName,
		authorEmail,
		header.AuthorDate,
		prName,
		prDesc,
		header.BodyAppendix,
		header.SHA,
		buf.String(),
	)
	if err != nil {
		return nil, err
	}

	err = tx.Commit()
	if err != nil {
		return nil, err
	}

	var pr PatchRequest
	err = cmd.Backend.DB.Get(&pr, "SELECT * FROM patch_requests WHERE id=?", prID)
	return &pr, err
}
