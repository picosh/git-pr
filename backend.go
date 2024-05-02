package git

import (
	"log/slog"
	"path/filepath"

	"github.com/charmbracelet/ssh"
	gossh "golang.org/x/crypto/ssh"
	// ssgit "github.com/charmbracelet/soft-serve/git"
	// "github.com/charmbracelet/soft-serve/pkg/utils"
)

type Backend struct {
	Logger *slog.Logger
	DB     *DB
	Cfg    *GitCfg
}

func (be *Backend) ReposDir() string {
	return filepath.Join(be.Cfg.DataPath, "repos")
}

func (be *Backend) RepoName(name string) string {
	return name + ".git"
}

func (be *Backend) Pubkey(pk ssh.PublicKey) string {
	return gossh.FingerprintSHA256(pk)
}
