package git

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"strings"

	"github.com/charmbracelet/ssh"
	"github.com/jmoiron/sqlx"
	gossh "golang.org/x/crypto/ssh"
)

type Backend struct {
	Logger *slog.Logger
	DB     *sqlx.DB
	Cfg    *GitCfg
}

var ErrRepoNoNamespace = fmt.Errorf("repo must be namespaced by username")

// Repo Namespace.
func (be *Backend) CreateRepoNs(userName, repoName string) string {
	if be.Cfg.CreateRepo == "admin" {
		return repoName
	}
	return fmt.Sprintf("%s/%s", userName, repoName)
}

func (be *Backend) ValidateRepoNs(repoNs string) error {
	_, repoID := be.SplitRepoNs(repoNs)
	if strings.Contains(repoID, "/") {
		return fmt.Errorf("repo can only contain a single forward-slash")
	}
	return nil
}

func (be *Backend) SplitRepoNs(repoNs string) (string, string) {
	results := strings.SplitN(repoNs, "/", 2)
	if len(results) == 1 {
		return "", results[0]
	}

	return results[0], results[1]
}

func (be *Backend) CanCreateRepo(repo *Repo, requester *User) error {
	pubkey, err := be.PubkeyToPublicKey(requester.Pubkey)
	if err != nil {
		return err
	}
	isAdmin := be.IsAdmin(pubkey)
	if isAdmin {
		return nil
	}

	// can create repo is a misnomer since we are saying it's ok to create
	// a repo even though one already exists.  this is a hack since this function
	// is used exclusively inside pr creation flow.
	if repo != nil {
		return nil
	}

	if be.Cfg.CreateRepo == "user" {
		return nil
	}

	// new repo with cfg indicating only admins can create prs/repos
	return fmt.Errorf("you are not authorized to create repo")
}

func (be *Backend) Pubkey(pk ssh.PublicKey) string {
	return be.KeyForKeyText(pk)
}

func (be *Backend) KeyForFingerprint(pk ssh.PublicKey) string {
	return gossh.FingerprintSHA256(pk)
}

func (be *Backend) PubkeyToPublicKey(pubkey string) (ssh.PublicKey, error) {
	kk, _, _, _, err := ssh.ParseAuthorizedKey([]byte(pubkey))
	return kk, err
}

func (be *Backend) KeyForKeyText(pk ssh.PublicKey) string {
	kb := base64.StdEncoding.EncodeToString(pk.Marshal())
	return fmt.Sprintf("%s %s", pk.Type(), kb)
}

func (be *Backend) KeysEqual(pka, pkb string) bool {
	return pka == pkb
}

func (be *Backend) IsAdmin(pk ssh.PublicKey) bool {
	for _, apk := range be.Cfg.Admins {
		if ssh.KeysEqual(pk, apk) {
			return true
		}
	}
	return false
}

func (be *Backend) IsPrOwner(pka, pkb int64) bool {
	return pka == pkb
}

type PrAcl struct {
	CanModify      bool
	CanDelete      bool
	CanReview      bool
	CanAddPatchset bool
}

func (be *Backend) GetPatchRequestAcl(repo *Repo, prq *PatchRequest, requester *User) *PrAcl {
	acl := &PrAcl{}
	if requester == nil {
		return acl
	}

	pubkey, err := be.PubkeyToPublicKey(requester.Pubkey)
	if err != nil {
		return acl
	}

	isAdmin := be.IsAdmin(pubkey)
	// admin can do it all
	if isAdmin {
		acl.CanModify = true
		acl.CanReview = true
		acl.CanDelete = true
		acl.CanAddPatchset = true
		return acl
	}

	// repo owner can do it all
	if repo.UserID == requester.ID {
		acl.CanModify = true
		acl.CanReview = true
		acl.CanDelete = true
		acl.CanAddPatchset = true
		return acl
	}

	// pr creator have special priv
	if be.IsPrOwner(prq.UserID, requester.ID) {
		acl.CanModify = true
		acl.CanReview = false
		acl.CanDelete = true
		acl.CanAddPatchset = true
		return acl
	}

	// otherwise no perms
	acl.CanModify = false
	acl.CanDelete = false
	// anyone can review or add a patchset
	acl.CanReview = true
	acl.CanAddPatchset = true

	return acl
}
