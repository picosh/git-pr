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
	wishrsync "github.com/picosh/send/send/rsync"
	"github.com/picosh/send/send/scp"
	"github.com/picosh/send/send/sftp"
)

func authHandler(ctx ssh.Context, key ssh.PublicKey) bool {
	return true
}

func GitSshServer() {
	host := os.Getenv("SSH_HOST")
	if host == "" {
		host = "0.0.0.0"
	}
	port := os.Getenv("SSH_PORT")
	if port == "" {
		port = "2222"
	}

	cfg := NewGitCfg()
	logger := slog.Default()
	handler := NewUploadHandler(cfg, logger)
	dbh, err := Open(":memory:", logger)
	if err != nil {
		panic(err)
	}
	dbh.Migrate()
	be := &Backend{
		DB:     dbh,
		Logger: logger,
		Cfg:    cfg,
	}

	s, err := wish.NewServer(
		wish.WithAddress(
			fmt.Sprintf("%s:%s", host, port),
		),
		wish.WithHostKeyPath(
			filepath.Join(cfg.DataPath, "term_info_ed25519"),
		),
		wish.WithPublicKeyAuth(authHandler),
		sftp.SSHOption(handler),
		wish.WithMiddleware(
			scp.Middleware(handler),
			wishrsync.Middleware(handler),
			GitServerMiddleware(be),
		),
	)

	if err != nil {
		logger.Error("could not create server", "err", err)
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	logger.Info("starting SSH server", "host", host, "port", port)
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
