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

type PatchsetOp int

const (
	OpNormal PatchsetOp = iota
	OpReview
	OpReplace
)

type GitPatchRequest interface {
	GetRepos() ([]Repo, error)
	GetRepoByID(repoID string) (*Repo, error)
	SubmitPatchRequest(repoID string, pubkey string, patchset io.Reader) (*PatchRequest, error)
	SubmitPatchSet(prID int64, pubkey string, op PatchsetOp, patchset io.Reader) ([]*Patch, error)
	GetPatchRequestByID(prID int64) (*PatchRequest, error)
	GetPatchRequests() ([]*PatchRequest, error)
	GetPatchRequestsByRepoID(repoID string) ([]*PatchRequest, error)
	GetPatchesByPrID(prID int64) ([]*Patch, error)
	UpdatePatchRequest(prID int64, pubkey, status string) error
	DeletePatchesByPrID(prID int64) error
	CreateEventLog(eventLog EventLog) error
	GetEventLogs() ([]*EventLog, error)
	GetEventLogsByRepoID(repoID string) ([]*EventLog, error)
	GetEventLogsByPrID(prID int64) ([]*EventLog, error)
	GetEventLogsByPubkey(pubkey string) ([]*EventLog, error)
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

// Status types: open, closed, accepted, reviewed.
func (cmd PrCmd) UpdatePatchRequest(prID int64, pubkey string, status string) error {
	_, err := cmd.Backend.DB.Exec(
		"UPDATE patch_requests SET status=? WHERE id=?",
		status,
		prID,
	)
	_ = cmd.CreateEventLog(EventLog{
		Pubkey:         pubkey,
		PatchRequestID: prID,
		Event:          "pr_status_changed",
		Data:           fmt.Sprintf(`{"status":"%s"}`, status),
	})
	return err
}

func (cmd PrCmd) CreateEventLog(eventLog EventLog) error {
	if eventLog.RepoID == "" && eventLog.PatchRequestID != 0 {
		var pr PatchRequest
		err := cmd.Backend.DB.Get(
			&pr,
			"SELECT repo_id FROM patch_requests WHERE id=?",
			eventLog.PatchRequestID,
		)
		if err != nil {
			cmd.Backend.Logger.Error(
				"could not find pr when creating eventLog",
				"err", err,
			)
			return nil
		}
		eventLog.RepoID = pr.RepoID
	}

	_, err := cmd.Backend.DB.Exec(
		"INSERT INTO event_logs (pubkey, repo_id, patch_request_id, comment_id, event, data) VALUES (?, ?, ?, ?, ?, ?)",
		eventLog.Pubkey,
		eventLog.RepoID,
		eventLog.PatchRequestID,
		eventLog.CommentID,
		eventLog.Event,
		eventLog.Data,
	)
	if err != nil {
		cmd.Backend.Logger.Error(
			"could not create eventLog",
			"err", err,
		)
	}
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
		dff := fmt.Sprintf(
			"%s->%s %s..%s %s->%s\n",
			diff.OldName, diff.NewName,
			diff.OldOIDPrefix, diff.NewOIDPrefix,
			diff.OldMode.String(), diff.NewMode.String(),
		)
		content += dff
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
			CommitSha:    header.SHA,
			ContentSha:   contentSha,
			RawText:      patchRaw,
		})
	}

	return patches, nil
}

func (cmd PrCmd) createPatch(tx *sqlx.Tx, review bool, patch *Patch) (int64, error) {
	patchExists := []Patch{}
	_ = cmd.Backend.DB.Select(&patchExists, "SELECT * FROM patches WHERE patch_request_id=? AND content_sha=?", patch.PatchRequestID, patch.ContentSha)
	if len(patchExists) > 0 {
		return 0, ErrPatchExists
	}

	var patchID int64
	row := tx.QueryRow(
		"INSERT INTO patches (pubkey, patch_request_id, author_name, author_email, author_date, title, body, body_appendix, commit_sha, content_sha, review, raw_text) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id",
		patch.Pubkey,
		patch.PatchRequestID,
		patch.AuthorName,
		patch.AuthorEmail,
		patch.AuthorDate,
		patch.Title,
		patch.Body,
		patch.BodyAppendix,
		patch.CommitSha,
		patch.ContentSha,
		review,
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
		_ = tx.Rollback()
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
		_, err = cmd.createPatch(tx, false, patch)
		if err != nil {
			return nil, err
		}
	}

	err = tx.Commit()
	if err != nil {
		return nil, err
	}

	_ = cmd.CreateEventLog(EventLog{
		Pubkey:         pubkey,
		PatchRequestID: prID,
		Event:          "pr_created",
	})

	var pr PatchRequest
	err = cmd.Backend.DB.Get(&pr, "SELECT * FROM patch_requests WHERE id=?", prID)
	return &pr, err
}

func (cmd PrCmd) SubmitPatchSet(prID int64, pubkey string, op PatchsetOp, patchset io.Reader) ([]*Patch, error) {
	fin := []*Patch{}
	tx, err := cmd.Backend.DB.Beginx()
	if err != nil {
		return fin, err
	}

	defer func() {
		_ = tx.Rollback()
	}()

	patches, err := cmd.parsePatchSet(patchset)
	if err != nil {
		return fin, err
	}

	if op == OpReplace {
		err = cmd.DeletePatchesByPrID(prID)
		if err != nil {
			return fin, err
		}
	}

	for _, patch := range patches {
		patch.Pubkey = pubkey
		patch.PatchRequestID = prID
		patchID, err := cmd.createPatch(tx, op == OpReview, patch)
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

	if len(fin) > 0 {
		event := "pr_patchset_added"
		if op == OpReview {
			event = "pr_reviewed"
		} else if op == OpReplace {
			event = "pr_patchset_replaced"
		}

		_ = cmd.CreateEventLog(EventLog{
			Pubkey:         pubkey,
			PatchRequestID: prID,
			Event:          event,
		})
	}

	return fin, err
}

func (cmd PrCmd) DeletePatchesByPrID(prID int64) error {
	_, err := cmd.Backend.DB.Exec(
		"DELETE FROM patches WHERE patch_request_id=?", prID,
	)
	return err
}

func (cmd PrCmd) GetEventLogs() ([]*EventLog, error) {
	eventLogs := []*EventLog{}
	err := cmd.Backend.DB.Select(
		&eventLogs,
		"SELECT * FROM event_logs ORDER BY created_at DESC",
	)
	return eventLogs, err
}

func (cmd PrCmd) GetEventLogsByRepoID(repoID string) ([]*EventLog, error) {
	eventLogs := []*EventLog{}
	err := cmd.Backend.DB.Select(
		&eventLogs,
		"SELECT * FROM event_logs WHERE repo_id=? ORDER BY created_at DESC",
		repoID,
	)
	return eventLogs, err
}

func (cmd PrCmd) GetEventLogsByPrID(prID int64) ([]*EventLog, error) {
	eventLogs := []*EventLog{}
	err := cmd.Backend.DB.Select(
		&eventLogs,
		"SELECT * FROM event_logs WHERE patch_request_id=? ORDER BY created_at DESC",
		prID,
	)
	return eventLogs, err
}

func (cmd PrCmd) GetEventLogsByPubkey(pubkey string) ([]*EventLog, error) {
	eventLogs := []*EventLog{}
	query := `SELECT * FROM event_logs
	WHERE pubkey=?
		OR patch_request_id IN (
			SELECT id FROM patch_requests WHERE pubkey=?
		)
	ORDER BY created_at DESC`
	err := cmd.Backend.DB.Select(
		&eventLogs,
		query,
		pubkey,
		pubkey,
	)
	return eventLogs, err
}
