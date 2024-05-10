package git

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
	"github.com/charmbracelet/soft-serve/pkg/git"
)

type GitPatchRequest interface {
	GetRepos() ([]string, error)
	SubmitPatchRequest(pubkey string, repoID string, patches io.Reader) (*PatchRequest, error)
	SubmitPatch(pubkey string, prID int64, review bool, patch io.Reader) (*Patch, error)
	GetPatchRequestByID(prID int64) (*PatchRequest, error)
	GetPatchRequests() ([]*PatchRequest, error)
	GetPatchesByPrID(prID int64) ([]*Patch, error)
	UpdatePatchRequest(prID int64, status string) error
}

type PrCmd struct {
	Backend *Backend
}

var _ GitPatchRequest = PrCmd{}
var _ GitPatchRequest = (*PrCmd)(nil)

func (pr PrCmd) GetRepos() ([]string, error) {
	repos := []string{}
	entries, err := os.ReadDir(pr.Backend.ReposDir())
	if err != nil {
		return repos, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			repos = append(repos, entry.Name())
		}
	}
	return repos, nil
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
	err := git.EnsureWithin(cmd.Backend.ReposDir(), repoID)
	if err != nil {
		return nil, err
	}
	loc := filepath.Join(cmd.Backend.ReposDir(), repoID)
	_, err = os.Stat(loc)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("repo does not exist: %s", loc)
	}

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
	row := cmd.Backend.DB.QueryRow(
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

	_, err = cmd.Backend.DB.Exec(
		"INSERT INTO patches (pubkey, patch_request_id, author_name, author_email, author_date, title, body, body_appendix, commit_sha, raw_text) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		pubkey,
		prID,
		header.Author.Name,
		header.Author.Email,
		header.AuthorDate,
		header.Title,
		header.Body,
		header.BodyAppendix,
		header.SHA,
		buf.String(),
	)
	if err != nil {
		return nil, err
	}

	var pr PatchRequest
	err = cmd.Backend.DB.Get(&pr, "SELECT * FROM patch_requests WHERE id=?", prID)
	return &pr, err
}
