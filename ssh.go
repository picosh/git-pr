package git

import (
	"context"
	"fmt"
	"log/slog"
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

func GitSshServer(cfg *GitCfg) {
	opts := &slog.HandlerOptions{
		AddSource: true,
	}
	logger := slog.New(
		slog.NewTextHandler(os.Stdout, opts),
	)
	dbh, err := Open(filepath.Join(cfg.DataPath, "pr.db"), logger)
	if err != nil {
		panic(err)
	}

	keys, err := getAuthorizedKeys(filepath.Join(cfg.DataPath, "authorized_keys"))
	if err == nil {
		cfg.Admins = keys
	} else {
		logger.Error("could not parse authorized keys file", "err", err)
	}

	be := &Backend{
		DB:     dbh,
		Logger: logger,
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
			filepath.Join(cfg.DataPath, "term_info_ed25519"),
		),
		wish.WithPublicKeyAuth(authHandler(prCmd)),
		wish.WithMiddleware(
			GitPatchRequestMiddleware(be, prCmd),
		),
	)

	if err != nil {
		logger.Error("could not create server", "err", err)
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	logger.Info("starting SSH server", "host", cfg.Host, "port", cfg.SshPort)
	go func() {
		if err = s.ListenAndServe(); err != nil {
			logger.Error("serve error", "err", err)
			os.Exit(1)
		}
	}()

	<-done
	logger.Info("stopping SSH server")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer func() { cancel() }()
	if err := s.Shutdown(ctx); err != nil {
		logger.Error("shutdown", "err", err)
		os.Exit(1)
	}
}
