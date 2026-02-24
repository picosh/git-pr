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

	// Web Server
	addr := fmt.Sprintf("%s:%s", cfg.Host, cfg.WebPort)
	web := git.GitWebServer(cfg)
	cfg.Logger.Info("starting web server", "addr", addr)
	go func() {
		if err := http.ListenAndServe(addr, web); err != nil {
			cfg.Logger.Error("listen", "err", err)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// SSH Server
	ssh := git.GitSshServer(ctx, cfg)

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	logger.Info("starting SSH server", "addr", ssh.Config.ListenAddr)
	go func() {
		if err := ssh.ListenAndServe(); err != nil {
			logger.Error("serve", "err", err.Error())
			os.Exit(1)
		}
	}()

	exit := func() {
		logger.Info("stopping ssh server")
		cancel()
	}

	<-done
	exit()
}
