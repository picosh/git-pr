package git

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/charmbracelet/soft-serve/pkg/utils"
	"github.com/charmbracelet/ssh"
	gossh "golang.org/x/crypto/ssh"
)

type Backend struct {
	Logger *slog.Logger
	DB     *DB
	Cfg    *GitCfg
}

func (be *Backend) ReposDir() string {
	return filepath.Join(be.Cfg.DataPath, "repos")
}

func (be *Backend) RepoName(id string) string {
	return utils.SanitizeRepo(id)
}

func (be *Backend) RepoID(name string) string {
	return name + ".git"
}

func (be *Backend) Pubkey(pk ssh.PublicKey) string {
	return be.KeyForKeyText(pk)
}

func (be *Backend) KeyForFingerprint(pk ssh.PublicKey) string {
	return gossh.FingerprintSHA256(pk)
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
