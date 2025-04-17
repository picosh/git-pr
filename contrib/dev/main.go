package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/picosh/git-pr"
	"github.com/picosh/git-pr/fixtures"
	"github.com/picosh/git-pr/util"
)

func main() {
	cleanupFlag := flag.Bool("cleanup", true, "Clean up tmp dir after quitting (default: true)")
	flag.Parse()

	opts := &slog.HandlerOptions{
		AddSource: true,
	}
	logger := slog.New(
		slog.NewTextHandler(os.Stdout, opts),
	)

	dataDir := util.CreateTmpDir()
	defer func() {
		if *cleanupFlag {
			os.RemoveAll(dataDir)
		}
	}()

	adminKey, userKey := util.GenerateKeys()
	cfgPath := util.CreateCfgFile(dataDir, cfgTmpl, adminKey)
	git.LoadConfigFile(cfgPath, logger)
	cfg := git.NewGitCfg(logger)

	s := git.GitSshServer(cfg)
	go s.ListenAndServe()
	time.Sleep(time.Millisecond * 100)
	w := git.GitWebServer(cfg)
	addr := fmt.Sprintf("%s:%s", cfg.Host, cfg.WebPort)
	go http.ListenAndServe(addr, w)

	// Hack to wait for startup
	time.Sleep(time.Millisecond * 100)

	patch, err := fixtures.Fixtures.ReadFile("single.patch")
	if err != nil {
		panic(err)
	}
	otherPatch, err := fixtures.Fixtures.ReadFile("with-cover.patch")
	if err != nil {
		panic(err)
	}

	// Accepted patch
	userKey.MustCmd(patch, "pr create test")
	userKey.MustCmd(nil, "pr edit 1 Accepted patch")
	adminKey.MustCmd(nil, "pr accept 1")

	// Closed patch (admin)
	userKey.MustCmd(patch, "pr create test")
	userKey.MustCmd(nil, "pr edit 2 Closed patch (admin)")
	adminKey.MustCmd(nil, "pr close 2")

	// Closed patch (contributor)
	userKey.MustCmd(patch, "pr create test")
	userKey.MustCmd(nil, "pr edit 3 Closed patch (contributor)")
	userKey.MustCmd(nil, "pr close 3")

	// Reviewed patch
	userKey.MustCmd(patch, "pr create test")
	userKey.MustCmd(nil, "pr edit 4 Reviewed patch")
	adminKey.MustCmd(otherPatch, "pr add --review 4")

	// Accepted patch with review
	userKey.MustCmd(patch, "pr create test")
	userKey.MustCmd(nil, "pr edit 5 Accepted patch with review")
	adminKey.MustCmd(otherPatch, "pr add --accept 5")

	// Closed patch with review
	userKey.MustCmd(patch, "pr create test")
	userKey.MustCmd(nil, "pr edit 6 Closed patch with review")
	adminKey.MustCmd(otherPatch, "pr add --close 6")

	fmt.Println("time to do some testing...")
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch
}

// args: tmpdir, adminKey
var cfgTmpl = `
url = "localhost"
data_dir = %q
admins = [%q]
time_format = "01/02/2006 15:04:05 07:00"
create_repo = "user"`
