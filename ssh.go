package git

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

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

func GitSshServer(cfg *GitCfg, killCh chan error) {
	dbpath := filepath.Join(cfg.DataDir, "pr.db")
	dbh, err := SqliteOpen(dbpath, cfg.Logger)
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
		return
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	cfg.Logger.Info("starting SSH server", "host", cfg.Host, "port", cfg.SshPort)
	go func() {
		if err = s.ListenAndServe(); err != nil {
			cfg.Logger.Error("serve error", "err", err)
			// os.Exit(1)
		}
	}()

	select {
	case <-done:
	case <-killCh:
	}
	cfg.Logger.Info("stopping SSH server")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer func() { cancel() }()
	if err := s.Shutdown(ctx); err != nil {
		cfg.Logger.Error("shutdown", "err", err)
		// os.Exit(1)
	}
}
