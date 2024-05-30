package git

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
	"github.com/jmoiron/sqlx"
)

var ErrPatchExists = errors.New("patch already exists for patch request")

type GitPatchRequest interface {
	GetRepos() ([]Repo, error)
	GetRepoByID(repoID string) (*Repo, error)
	SubmitPatchRequest(repoID string, pubkey string, patchset io.Reader) (*PatchRequest, error)
	SubmitPatchSet(prID int64, pubkey string, review bool, patchset io.Reader) ([]*Patch, error)
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

// calcContentSha calculates a shasum containing the important content
// changes related to a patch.
// We cannot rely on patch.CommitSha because it includes the commit date
// that will change when a user fetches and applies the patch locally.
func (cmd PrCmd) calcContentSha(diffFiles []*gitdiff.File, header *gitdiff.PatchHeader) string {
	authorName := ""
	authorEmail := ""
	if header.Author != nil {
		authorName = header.Author.Name
		authorEmail = header.Author.Email
	}
	content := fmt.Sprintf(
		"%s\n%s\n%s\n%s\n%s\n",
		header.Title,
		header.Body,
		authorName,
		authorEmail,
		header.AuthorDate,
	)
	for _, diff := range diffFiles {
		content += fmt.Sprintf(
			"%s->%s %s..%s %s-%s\n",
			diff.OldName, diff.NewName,
			diff.OldOIDPrefix, diff.NewOIDPrefix,
			diff.OldMode.String(), diff.NewMode.String(),
		)
	}
	sha := sha256.Sum256([]byte(content))
	shaStr := hex.EncodeToString(sha[:])
	return shaStr
}

func (cmd PrCmd) splitPatchSet(patchset string) []string {
	return strings.Split(patchset, "\n\n\n")
}

func (cmd PrCmd) parsePatchSet(patchset io.Reader) ([]*Patch, error) {
	patches := []*Patch{}
	buf := new(strings.Builder)
	_, err := io.Copy(buf, patchset)
	if err != nil {
		return nil, err
	}

	patchesRaw := cmd.splitPatchSet(buf.String())
	for _, patchRaw := range patchesRaw {
		reader := strings.NewReader(patchRaw)
		diffFiles, preamble, err := gitdiff.Parse(reader)
		if err != nil {
			return nil, err
		}
		header, err := gitdiff.ParsePatchHeader(preamble)
		if err != nil {
			return nil, err
		}

		authorName := "Unknown"
		authorEmail := ""
		if header.Author != nil {
			authorName = header.Author.Name
			authorEmail = header.Author.Email
		}

		contentSha := cmd.calcContentSha(diffFiles, header)

		patches = append(patches, &Patch{
			AuthorName:   authorName,
			AuthorEmail:  authorEmail,
			AuthorDate:   header.AuthorDate.UTC().String(),
			Title:        header.Title,
			Body:         header.Body,
			BodyAppendix: header.BodyAppendix,
			ContentSha:   contentSha,
			CommitSha:    header.SHA,
			RawText:      patchRaw,
		})
	}

	return patches, nil
}

func (cmd PrCmd) createPatch(tx *sqlx.Tx, patch *Patch) (int64, error) {
	var patchExists *Patch
	_ = tx.Select(&patchExists, "SELECT * FROM patches WHERE patch_request_id = ? AND content_sha = ?", patch.PatchRequestID, patch.ContentSha)
	if patchExists.ID == 0 {
		return 0, ErrPatchExists
	}

	var patchID int64
	row := tx.QueryRow(
		"INSERT INTO patches (pubkey, patch_request_id, author_name, author_email, author_date, title, body, body_appendix, commit_sha, content_sha, raw_text) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		patch.Pubkey,
		patch.PatchRequestID,
		patch.AuthorName,
		patch.AuthorEmail,
		patch.AuthorDate,
		patch.Title,
		patch.Body,
		patch.BodyAppendix,
		patch.ContentSha,
		patch.CommitSha,
		patch.RawText,
	)
	err := row.Scan(&patchID)
	if err != nil {
		return 0, err
	}
	if patchID == 0 {
		return 0, fmt.Errorf("could not create patch request")
	}
	return patchID, err
}

func (cmd PrCmd) SubmitPatchRequest(repoID string, pubkey string, patchset io.Reader) (*PatchRequest, error) {
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

	patches, err := cmd.parsePatchSet(patchset)
	if err != nil {
		return nil, err
	}
	prName := ""
	prText := ""
	if len(patches) > 0 {
		prName = patches[0].Title
		prText = patches[0].Body
	}

	var prID int64
	row := tx.QueryRow(
		"INSERT INTO patch_requests (pubkey, repo_id, name, text, status, updated_at) VALUES(?, ?, ?, ?, ?, ?) RETURNING id",
		pubkey,
		repoID,
		prName,
		prText,
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

	for _, patch := range patches {
		patch.Pubkey = pubkey
		patch.PatchRequestID = prID
		_, err = cmd.createPatch(tx, patch)
		if err != nil {
			return nil, err
		}
	}

	err = tx.Commit()
	if err != nil {
		return nil, err
	}

	var pr PatchRequest
	err = cmd.Backend.DB.Get(&pr, "SELECT * FROM patch_requests WHERE id=?", prID)
	return &pr, err
}

func (cmd PrCmd) SubmitPatchSet(prID int64, pubkey string, review bool, patchset io.Reader) ([]*Patch, error) {
	fin := []*Patch{}
	tx, err := cmd.Backend.DB.Beginx()
	if err != nil {
		return fin, err
	}

	defer func() {
		err := tx.Rollback()
		if err != nil {
			cmd.Backend.Logger.Error("rollback", "err", err)
		}
	}()

	patches, err := cmd.parsePatchSet(patchset)
	if err != nil {
		return fin, err
	}

	for _, patch := range patches {
		patch.Pubkey = pubkey
		patch.PatchRequestID = prID
		patchID, err := cmd.createPatch(tx, patch)
		if err == nil {
			patch.ID = patchID
			fin = append(fin, patch)
		} else {
			if !errors.Is(ErrPatchExists, err) {
				return fin, err
			}
		}
	}

	err = tx.Commit()
	if err != nil {
		return fin, err
	}

	return fin, err
}
