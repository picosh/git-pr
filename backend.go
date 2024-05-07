package git

import (
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
	return gossh.FingerprintSHA256(pk)
}

func (be *Backend) IsAdmin(pk ssh.PublicKey) bool {
	for _, apk := range be.Cfg.Admins {
		if ssh.KeysEqual(pk, apk) {
			return true
		}
	}
	return false
}
