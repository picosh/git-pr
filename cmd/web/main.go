package main

import (
	"flag"
	"log/slog"
	"os"

	git "github.com/picosh/git-pr"
)

func main() {
	fpath := flag.String("config", "example.toml", "configuration toml file")
	flag.Parse()
	opts := &slog.HandlerOptions{
		AddSource: true,
	}
	logger := slog.New(
		slog.NewTextHandler(os.Stdout, opts),
	)
	git.StartWebServer(git.NewGitCfg(*fpath, logger))
}
