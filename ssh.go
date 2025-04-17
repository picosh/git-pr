package git

import (
	"fmt"
	"path/filepath"

	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
)

func authHandler(pr *PrCmd) func(ctx ssh.Context, key ssh.PublicKey) bool {
	return func(ctx ssh.Context, key ssh.PublicKey) bool {
		pubkey := pr.Backend.Pubkey(key)
		userName := ctx.User()
		err := pr.IsBanned(pubkey, userName)
		if err != nil {
			pr.Backend.Logger.Info(
				"user denied access",
				"err", err,
				"username", userName,
				"pubkey", pubkey,
			)
			return false
		}
		return true
	}
}

func GitSshServer(cfg *GitCfg) *ssh.Server {
	dbpath := filepath.Join(cfg.DataDir, "pr.db?_fk=on")
	dbh, err := SqliteOpen("file:"+dbpath, cfg.Logger)
	if err != nil {
		panic(fmt.Sprintf("cannot find database file, check folder and perms: %s: %s", dbpath, err))
	}

	be := &Backend{
		DB:     dbh,
		Logger: cfg.Logger,
		Cfg:    cfg,
	}
	prCmd := &PrCmd{
		Backend: be,
	}

	s, err := wish.NewServer(
		wish.WithAddress(
			fmt.Sprintf("%s:%s", cfg.Host, cfg.SshPort),
		),
		wish.WithHostKeyPath(
			filepath.Join(cfg.DataDir, "term_info_ed25519"),
		),
		wish.WithPublicKeyAuth(authHandler(prCmd)),
		wish.WithMiddleware(
			GitPatchRequestMiddleware(be, prCmd),
		),
	)
	if err != nil {
		cfg.Logger.Error("could not create server", "err", err)
		return nil
	}
	return s
}
