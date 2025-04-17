package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	git "github.com/picosh/git-pr"
)

func main() {
	fpath := flag.String("config", "git-pr.toml", "configuration toml file")
	flag.Parse()
	opts := &slog.HandlerOptions{
		AddSource: true,
	}
	logger := slog.New(
		slog.NewTextHandler(os.Stdout, opts),
	)
	git.LoadConfigFile(*fpath, logger)
	cfg := git.NewGitCfg(logger)

	// SSH Server
	ssh := git.GitSshServer(cfg)
	cfg.Logger.Info("starting SSH server", "host", cfg.Host, "port", cfg.SshPort)
	go func() {
		if err := ssh.ListenAndServe(); err != nil {
			cfg.Logger.Error("serve error", "err", err)
		}
	}()

	// Web Server
	addr := fmt.Sprintf("%s:%s", cfg.Host, cfg.WebPort)
	web := git.GitWebServer(cfg)
	cfg.Logger.Info("starting web server", "addr", addr)
	go func() {
		if err := http.ListenAndServe(addr, web); err != nil {
			cfg.Logger.Error("listen", "err", err)
		}
	}()

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	<-done
	cfg.Logger.Info("stopping SSH server")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer func() { cancel() }()
	if err := ssh.Shutdown(ctx); err != nil {
		cfg.Logger.Error("shutdown", "err", err)
	}
}
