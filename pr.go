package git

import (
	"errors"
	"fmt"
	"io"
	"time"

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
	GetUsers() ([]*User, error)
	GetUserByID(userID int64) (*User, error)
	GetUserByPubkey(pubkey string) (*User, error)
	UpsertUser(pubkey, name string) (*User, error)
	IsBanned(pubkey, ipAddress string) error
	GetRepos() ([]*Repo, error)
	GetReposWithLatestPr() ([]RepoWithLatestPr, error)
	GetRepoByID(repoID string) (*Repo, error)
	SubmitPatchRequest(repoID string, userID int64, patchset io.Reader) (*PatchRequest, error)
	SubmitPatchset(prID, userID int64, op PatchsetOp, patchset io.Reader) ([]*Patch, error)
	GetPatchRequestByID(prID int64) (*PatchRequest, error)
	GetPatchRequests() ([]*PatchRequest, error)
	GetPatchRequestsByRepoID(repoID string) ([]*PatchRequest, error)
	GetPatchsetsByPrID(prID int64) ([]*Patchset, error)
	GetLatestPatchsetByPrID(prID int64) (*Patchset, error)
	GetPatchesByPatchsetID(prID int64) ([]*Patch, error)
	UpdatePatchRequestStatus(prID, userID int64, status string) error
	UpdatePatchRequestName(prID, userID int64, name string) error
	DeletePatchesByPrID(prID int64) error
	CreateEventLog(eventLog EventLog) error
	GetEventLogs() ([]*EventLog, error)
	GetEventLogsByRepoID(repoID string) ([]*EventLog, error)
	GetEventLogsByPrID(prID int64) ([]*EventLog, error)
	GetEventLogsByUserID(userID int64) ([]*EventLog, error)
}

type PrCmd struct {
	Backend *Backend
}

var _ GitPatchRequest = PrCmd{}
var _ GitPatchRequest = (*PrCmd)(nil)

func (pr PrCmd) IsBanned(pubkey, ipAddress string) error {
	acl := []*Acl{}
	err := pr.Backend.DB.Select(
		&acl,
		"SELECT * FROM acl WHERE permission='banned' AND (pubkey=? OR ip_address=?)",
		pubkey,
		ipAddress,
	)
	if len(acl) > 0 {
		return fmt.Errorf("user has been banned")
	}
	return err
}

func (pr PrCmd) GetUsers() ([]*User, error) {
	users := []*User{}
	err := pr.Backend.DB.Select(&users, "SELECT * FROM app_users")
	return users, err
}

func (pr PrCmd) GetUserByID(id int64) (*User, error) {
	var user User
	err := pr.Backend.DB.Get(&user, "SELECT * FROM app_users WHERE id=?", id)
	return &user, err
}

func (pr PrCmd) GetUserByPubkey(pubkey string) (*User, error) {
	var user User
	err := pr.Backend.DB.Get(&user, "SELECT * FROM app_users WHERE pubkey=?", pubkey)
	return &user, err
}

func (pr PrCmd) computeUserName(name string) (string, error) {
	var user User
	err := pr.Backend.DB.Get(&user, "SELECT * FROM app_users WHERE name=?", name)
	if err != nil {
		return name, nil
	}
	// collision, generate random number and append
	return fmt.Sprintf("%s%s", name, randSeq(4)), nil
}

func (pr PrCmd) createUser(pubkey, name string) (*User, error) {
	if pubkey == "" {
		return nil, fmt.Errorf("must provide pubkey when creating user")
	}
	if name == "" {
		return nil, fmt.Errorf("must provide user name when creating user")
	}

	userName, err := pr.computeUserName(name)
	if err != nil {
		pr.Backend.Logger.Error("could not compute username", "err", err)
	}

	var userID int64
	row := pr.Backend.DB.QueryRow(
		"INSERT INTO app_users (pubkey, name) VALUES (?, ?) RETURNING id",
		pubkey,
		userName,
	)
	err = row.Scan(&userID)
	if err != nil {
		return nil, err
	}
	if userID == 0 {
		return nil, fmt.Errorf("could not create user")
	}

	user, err := pr.GetUserByID(userID)
	return user, err
}

func (pr PrCmd) UpsertUser(pubkey, name string) (*User, error) {
	if pubkey == "" {
		return nil, fmt.Errorf("must provide pubkey during upsert")
	}
	user, err := pr.GetUserByPubkey(pubkey)
	if err != nil {
		user, err = pr.createUser(pubkey, name)
	}
	return user, err
}

type PrWithRepo struct {
	LastUpdatedPrID int64
	RepoID          string
}

type RepoWithLatestPr struct {
	*Repo
	User         *User
	PatchRequest *PatchRequest
}

func (pr PrCmd) GetRepos() ([]*Repo, error) {
	return pr.Backend.Cfg.Repos, nil
}

func (pr PrCmd) GetReposWithLatestPr() ([]RepoWithLatestPr, error) {
	repos := []RepoWithLatestPr{}
	prs := []PatchRequest{}
	err := pr.Backend.DB.Select(&prs, "SELECT *, max(updated_at) as last_updated FROM patch_requests GROUP BY repo_id")
	if err != nil {
		return repos, err
	}

	users, err := pr.GetUsers()
	if err != nil {
		return repos, err
	}

	// we want recently modified repos to be on top
	for _, prq := range prs {
		for _, repo := range pr.Backend.Cfg.Repos {
			if prq.RepoID == repo.ID {
				var user *User
				for _, usr := range users {
					if prq.UserID == usr.ID {
						user = usr
						break
					}
				}
				repos = append(repos, RepoWithLatestPr{
					User:         user,
					Repo:         repo,
					PatchRequest: &prq,
				})
			}
		}
	}

	for _, repo := range pr.Backend.Cfg.Repos {
		found := false
		for _, curRepo := range repos {
			if curRepo.ID == repo.ID {
				found = true
			}
		}
		if !found {
			repos = append(repos, RepoWithLatestPr{
				Repo: repo,
			})
		}
	}

	return repos, nil
}

func (pr PrCmd) GetRepoByID(repoID string) (*Repo, error) {
	repos, err := pr.GetRepos()
	if err != nil {
		return nil, err
	}

	for _, repo := range repos {
		if repo.ID == repoID {
			return repo, nil
		}
	}

	return nil, fmt.Errorf("repo not found: %s", repoID)
}

func (pr PrCmd) GetPatchsetsByPrID(prID int64) ([]*Patchset, error) {
	patchsets := []*Patchset{}
	err := pr.Backend.DB.Select(
		&patchsets,
		"SELECT * FROM patchsets WHERE patch_request_id=? ORDER BY created_at ASC",
		prID,
	)
	if err != nil {
		return patchsets, err
	}
	if len(patchsets) == 0 {
		return patchsets, fmt.Errorf("no patchsets found for patch request: %d", prID)
	}
	return patchsets, nil
}

func (pr PrCmd) GetLatestPatchsetByPrID(prID int64) (*Patchset, error) {
	patchsets, err := pr.GetPatchsetsByPrID(prID)
	if err != nil {
		return nil, err
	}
	if len(patchsets) == 0 {
		return nil, fmt.Errorf("not patchsets found for patch request: %d", prID)
	}
	return patchsets[len(patchsets)-1], nil
}

func (pr PrCmd) GetPatchesByPatchsetID(patchsetID int64) ([]*Patch, error) {
	patches := []*Patch{}
	err := pr.Backend.DB.Select(
		&patches,
		"SELECT * FROM patches WHERE patchset_id=? ORDER BY created_at ASC, id ASC",
		patchsetID,
	)
	return patches, err
}

func (cmd PrCmd) GetPatchRequests() ([]*PatchRequest, error) {
	prs := []*PatchRequest{}
	err := cmd.Backend.DB.Select(
		&prs,
		"SELECT * FROM patch_requests ORDER BY created_at DESC",
	)
	return prs, err
}

func (cmd PrCmd) GetPatchRequestsByRepoID(repoID string) ([]*PatchRequest, error) {
	prs := []*PatchRequest{}
	err := cmd.Backend.DB.Select(
		&prs,
		"SELECT * FROM patch_requests WHERE repo_id=? ORDER BY created_at DESC",
		repoID,
	)
	return prs, err
}

func (cmd PrCmd) GetPatchRequestByID(prID int64) (*PatchRequest, error) {
	pr := PatchRequest{}
	err := cmd.Backend.DB.Get(
		&pr,
		"SELECT * FROM patch_requests WHERE id=? ORDER BY created_at DESC",
		prID,
	)
	return &pr, err
}

// Status types: open, closed, accepted, reviewed.
func (cmd PrCmd) UpdatePatchRequestStatus(prID int64, userID int64, status string) error {
	_, err := cmd.Backend.DB.Exec(
		"UPDATE patch_requests SET status=? WHERE id=?",
		status,
		prID,
	)
	_ = cmd.CreateEventLog(EventLog{
		UserID:         userID,
		PatchRequestID: prID,
		Event:          "pr_status_changed",
		Data:           fmt.Sprintf(`{"status":"%s"}`, status),
	})
	return err
}

func (cmd PrCmd) UpdatePatchRequestName(prID int64, userID int64, name string) error {
	if name == "" {
		return fmt.Errorf("must provide name or text in order to update patch request")
	}

	_, err := cmd.Backend.DB.Exec(
		"UPDATE patch_requests SET name=? WHERE id=?",
		name,
		prID,
	)
	_ = cmd.CreateEventLog(EventLog{
		UserID:         userID,
		PatchRequestID: prID,
		Event:          "pr_name_changed",
		Data:           fmt.Sprintf(`{"name":"%s"}`, name),
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
		"INSERT INTO event_logs (user_id, repo_id, patch_request_id, event, data) VALUES (?, ?, ?, ?, ?)",
		eventLog.UserID,
		eventLog.RepoID,
		eventLog.PatchRequestID,
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

func (cmd PrCmd) createPatch(tx *sqlx.Tx, patch *Patch) (int64, error) {
	patchExists := []Patch{}
	_ = cmd.Backend.DB.Select(&patchExists, "SELECT * FROM patches WHERE patchset_id=? AND content_sha=?", patch.PatchsetID, patch.ContentSha)
	if len(patchExists) > 0 {
		return 0, ErrPatchExists
	}

	var patchID int64
	row := tx.QueryRow(
		"INSERT INTO patches (user_id, patchset_id, author_name, author_email, author_date, title, body, body_appendix, commit_sha, content_sha, base_commit_sha, raw_text) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id",
		patch.UserID,
		patch.PatchsetID,
		patch.AuthorName,
		patch.AuthorEmail,
		patch.AuthorDate,
		patch.Title,
		patch.Body,
		patch.BodyAppendix,
		patch.CommitSha,
		patch.ContentSha,
		patch.BaseCommitSha,
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

func (cmd PrCmd) SubmitPatchRequest(repoID string, userID int64, patchset io.Reader) (*PatchRequest, error) {
	tx, err := cmd.Backend.DB.Beginx()
	if err != nil {
		return nil, err
	}

	defer func() {
		_ = tx.Rollback()
	}()

	patches, err := parsePatchset(patchset)
	if err != nil {
		return nil, err
	}

	if len(patches) == 0 {
		return nil, fmt.Errorf("after parsing patchset we did't find any patches, did you send us an empty patchset?")
	}

	_, err = cmd.GetRepoByID(repoID)
	if err != nil {
		return nil, fmt.Errorf("repo does not exist")
	}

	prName := ""
	prText := ""
	if len(patches) > 0 {
		prName = patches[0].Title
		prText = patches[0].Body
	}

	var prID int64
	row := tx.QueryRow(
		"INSERT INTO patch_requests (user_id, repo_id, name, text, status, updated_at) VALUES(?, ?, ?, ?, ?, ?) RETURNING id",
		userID,
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

	var patchsetID int64
	row = tx.QueryRow(
		"INSERT INTO patchsets (user_id, patch_request_id) VALUES(?, ?) RETURNING id",
		userID,
		prID,
	)
	err = row.Scan(&patchsetID)
	if err != nil {
		return nil, err
	}
	if patchsetID == 0 {
		return nil, fmt.Errorf("could not create patchset")
	}

	for _, patch := range patches {
		patch.UserID = userID
		patch.PatchsetID = prID
		_, err = cmd.createPatch(tx, patch)
		if err != nil {
			return nil, err
		}
	}

	err = tx.Commit()
	if err != nil {
		return nil, err
	}

	_ = cmd.CreateEventLog(EventLog{
		UserID:         userID,
		PatchRequestID: prID,
		Event:          "pr_created",
	})

	var pr PatchRequest
	err = cmd.Backend.DB.Get(&pr, "SELECT * FROM patch_requests WHERE id=?", prID)
	return &pr, err
}

func (cmd PrCmd) SubmitPatchset(prID int64, userID int64, op PatchsetOp, patchset io.Reader) ([]*Patch, error) {
	fin := []*Patch{}
	tx, err := cmd.Backend.DB.Beginx()
	if err != nil {
		return fin, err
	}

	defer func() {
		_ = tx.Rollback()
	}()

	patches, err := parsePatchset(patchset)
	if err != nil {
		return fin, err
	}

	if op == OpReplace {
		err = cmd.DeletePatchesByPrID(prID)
		if err != nil {
			return fin, err
		}
	}

	var patchsetID int64
	row := tx.QueryRow(
		"INSERT INTO patchsets (user_id, patch_request_id, review) VALUES(?, ?, ?) RETURNING id",
		userID,
		prID,
		op == OpReview,
	)
	err = row.Scan(&patchsetID)
	if err != nil {
		return nil, err
	}
	if patchsetID == 0 {
		return nil, fmt.Errorf("could not create patchset")
	}

	for _, patch := range patches {
		patch.UserID = userID
		patch.PatchsetID = patchsetID
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

	if len(fin) > 0 {
		event := "pr_patchset_added"
		if op == OpReview {
			event = "pr_reviewed"
		} else if op == OpReplace {
			event = "pr_patchset_replaced"
		}

		_ = cmd.CreateEventLog(EventLog{
			UserID:         userID,
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

func (cmd PrCmd) GetEventLogsByUserID(userID int64) ([]*EventLog, error) {
	eventLogs := []*EventLog{}
	query := `SELECT * FROM event_logs
	WHERE user_id=?
		OR patch_request_id IN (
			SELECT id FROM patch_requests WHERE user_id=?
		)
	ORDER BY created_at DESC`
	err := cmd.Backend.DB.Select(
		&eventLogs,
		query,
		userID,
		userID,
	)
	return eventLogs, err
}
