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

	"github.com/charmbracelet/soft-serve/pkg/git"
	"github.com/charmbracelet/soft-serve/pkg/utils"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
)

func authHandler(ctx ssh.Context, key ssh.PublicKey) bool {
	return true
}

func GitServerMiddleware(cfg *GitCfg) wish.Middleware {
	return func(next ssh.Handler) ssh.Handler {
		return func(sesh ssh.Session) {
			_, _, activePty := sesh.Pty()
			if activePty {
				next(sesh)
				return
			}

			args := sesh.Command()
			cmd := args[0]
			fmt.Println(args)

			name := utils.SanitizeRepo(args[1])
			// git bare repositories should end in ".git"
			// https://git-scm.com/docs/gitrepository-layout
			repoDir := name + ".git"
			reposDir := filepath.Join(cfg.DataPath, "repos")
			err := git.EnsureWithin(reposDir, repoDir)
			if err != nil {
				wish.Fatal(sesh, err.Error())
			}
			repoPath := filepath.Join(reposDir, repoDir)
			serviceCmd := git.ServiceCommand{
				Stdin:  sesh,
				Stdout: sesh,
				Stderr: sesh.Stderr(),
				Dir:    repoPath,
				Env:    sesh.Environ(),
			}

			if cmd == "git-receive-pack" {
				err := git.ReceivePack(sesh.Context(), serviceCmd)
				if err != nil {
					wish.Fatal(sesh, err.Error())
					return
				}
				return
			} else if cmd == "git-upload-pack" {
				err := git.UploadPack(sesh.Context(), serviceCmd)
				if err != nil {
					wish.Fatal(sesh, err.Error())
					return
				}
				return
			}
		}
	}
}

type GitCfg struct {
	DataPath string
}

func NewGitCfg() *GitCfg {
	return &GitCfg{
		DataPath: "ssh_data",
	}
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

	s, err := wish.NewServer(
		wish.WithAddress(fmt.Sprintf("%s:%s", host, port)),
		wish.WithHostKeyPath(filepath.Join(cfg.DataPath, "term_info_ed25519")),
		wish.WithPublicKeyAuth(authHandler),
		wish.WithMiddleware(GitServerMiddleware(cfg)),
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
