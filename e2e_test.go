package git

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/picosh/git-pr/fixtures"
	"github.com/picosh/git-pr/util"
)

func TestE2E(t *testing.T) {
	testSingleTenantE2E(t)
	testMultiTenantE2E(t)
}

func testSingleTenantE2E(t *testing.T) {
	t.Log("single tenant end-to-end tests")
	dataDir := util.CreateTmpDir()
	defer func() {
		os.RemoveAll(dataDir)
	}()
	suite := setupTest(dataDir, cfgSingleTenantTmpl)
	done := make(chan error)
	go GitSshServer(suite.cfg, done)
	// Hack to wait for startup
	time.Sleep(time.Millisecond * 100)
	_, err := suite.userKey.Cmd(suite.patch, "pr create test")
	if err == nil {
		t.Error("user should not be able to create a PR")
	}
	suite.adminKey.MustCmd(suite.patch, "pr create test")

	// Snapshot test ls command
	actual, err := suite.userKey.Cmd(nil, "pr ls")
	bail(err)
	snaps.MatchSnapshot(t, actual)

	done <- nil
}

func testMultiTenantE2E(t *testing.T) {
	t.Log("multi tenant end-to-end tests")
	dataDir := util.CreateTmpDir()
	defer func() {
		os.RemoveAll(dataDir)
	}()
	suite := setupTest(dataDir, cfgMultiTenantTmpl)
	done := make(chan error)
	go GitSshServer(suite.cfg, done)

	time.Sleep(time.Millisecond * 100)

	// Accepted pr
	suite.userKey.MustCmd(suite.patch, "pr create test")
	suite.userKey.MustCmd(nil, "pr edit 1 Accepted patch")
	_, err := suite.userKey.Cmd(nil, "pr accept 1")
	if err == nil {
		t.Error("contrib should not be able to accept their own PR")
	}
	suite.adminKey.MustCmd(nil, "pr accept 1")

	// Closed pr (admin)
	suite.userKey.MustCmd(suite.patch, "pr create test")
	suite.userKey.MustCmd(nil, "pr edit 2 Closed patch (admin)")
	suite.adminKey.MustCmd(nil, "pr close 2")

	// Closed pr (contributor)
	suite.userKey.MustCmd(suite.patch, "pr create test")
	suite.userKey.MustCmd(nil, "pr edit 3 Closed patch (contributor)")
	suite.userKey.MustCmd(nil, "pr close 3")

	// Reviewed pr
	suite.userKey.MustCmd(suite.patch, "pr create test")
	suite.userKey.MustCmd(nil, "pr edit 4 Reviewed patch")
	suite.adminKey.MustCmd(suite.otherPatch, "pr add --review 4")

	// Accepted pr with review
	suite.userKey.MustCmd(suite.patch, "pr create test")
	suite.userKey.MustCmd(nil, "pr edit 5 Accepted patch with review")
	suite.adminKey.MustCmd(suite.otherPatch, "pr add --accept 5")

	// Closed pr with review
	suite.userKey.MustCmd(suite.patch, "pr create test")
	suite.userKey.MustCmd(nil, "pr edit 6 Closed patch with review")
	suite.adminKey.MustCmd(suite.otherPatch, "pr add --close 6")

	// Create pr with user namespace
	suite.adminKey.MustCmd(nil, "repo create ai")
	suite.userKey.MustCmd(suite.patch, "pr create admin/ai")
	suite.adminKey.MustCmd(suite.otherPatch, "pr add --accept 7")

	// Create pr with default `bin` repo
	actual, err := suite.userKey.Cmd(suite.patch, "pr create")
	bail(err)
	snaps.MatchSnapshot(t, actual)

	// Snapshot test ls command
	actual, err = suite.userKey.Cmd(nil, "pr ls")
	bail(err)
	snaps.MatchSnapshot(t, actual)

	// Snapshot test logs command
	actual, err = suite.userKey.Cmd(nil, "logs --repo admin/ai")
	bail(err)
	snaps.MatchSnapshot(t, actual)

	done <- nil
}

type TestSuite struct {
	cfg        *GitCfg
	userKey    util.UserSSH
	adminKey   util.UserSSH
	patch      []byte
	otherPatch []byte
}

func setupTest(dataDir string, cfgTmpl string) TestSuite {
	opts := &slog.HandlerOptions{
		AddSource: true,
	}
	logger := slog.New(
		slog.NewTextHandler(os.Stdout, opts),
	)

	adminKey, userKey := util.GenerateKeys()
	cfgPath := util.CreateCfgFile(dataDir, cfgTmpl, adminKey)
	LoadConfigFile(cfgPath, logger)
	cfg := NewGitCfg(logger)

	// so outputs dont show dates
	cfg.TimeFormat = ""

	patch, err := fixtures.Fixtures.ReadFile("single.patch")
	if err != nil {
		panic(err)
	}
	otherPatch, err := fixtures.Fixtures.ReadFile("with-cover.patch")
	if err != nil {
		panic(err)
	}

	return TestSuite{cfg, userKey, adminKey, patch, otherPatch}
}

var cfgSingleTenantTmpl = `
url = "localhost"
data_dir = %q
admins = [%q]
time_format = "01/02/2006 15:04:05 07:00"
create_repo = "admin"`

var cfgMultiTenantTmpl = `
url = "localhost"
data_dir = %q
admins = [%q]
time_format = "01/02/2006 15:04:05 07:00"
create_repo = "user"`
