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

	t.Log("User cannot create repo")
	_, err := suite.userKey.Cmd(suite.patch, "pr create test")
	if err == nil {
		t.Fatal("user should not be able to create a PR")
	}
	suite.adminKey.MustCmd(suite.patch, "pr create test")

	t.Log("User should be able to create a patch")
	suite.userKey.MustCmd(suite.patch, "pr create test")

	t.Log("Snapshot test ls command")
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

	t.Log("Admin should be able to create a repo")
	suite.adminKey.MustCmd(nil, "repo create test")

	t.Log("Accepted pr")
	suite.userKey.MustCmd(suite.patch, "pr create admin/test")
	suite.userKey.MustCmd(nil, "pr edit 1 Accepted patch")
	_, err := suite.userKey.Cmd(nil, "pr accept 1")
	if err == nil {
		t.Fatal("contrib should not be able to accept their own PR")
	}
	suite.adminKey.MustCmd(nil, "pr accept 1")

	t.Log("Closed pr (admin)")
	suite.userKey.MustCmd(suite.patch, "pr create test")
	suite.userKey.MustCmd(nil, "pr edit 2 Closed patch (admin)")
	suite.adminKey.MustCmd(nil, "pr close 2")

	t.Log("Closed pr (contributor)")
	suite.userKey.MustCmd(suite.patch, "pr create test")
	suite.userKey.MustCmd(nil, "pr edit 3 Closed patch (contributor)")
	suite.userKey.MustCmd(nil, "pr close 3")

	t.Log("Reviewed pr")
	suite.userKey.MustCmd(suite.patch, "pr create test")
	suite.userKey.MustCmd(nil, "pr edit 4 Reviewed patch")
	suite.adminKey.MustCmd(suite.otherPatch, "pr add --review 4")

	t.Log("Accepted pr with review")
	suite.userKey.MustCmd(suite.patch, "pr create test")
	suite.userKey.MustCmd(nil, "pr edit 5 Accepted patch with review")
	suite.adminKey.MustCmd(suite.otherPatch, "pr add --accept 5")

	t.Log("Closed pr with review")
	suite.userKey.MustCmd(suite.patch, "pr create test")
	suite.userKey.MustCmd(nil, "pr edit 6 Closed patch with review")
	suite.adminKey.MustCmd(suite.otherPatch, "pr add --close 6")

	t.Log("Create pr with user repo and user can accept")
	suite.userKey.MustCmd(nil, "repo create ai")
	suite.adminKey.MustCmd(suite.patch, "pr create contributor/ai")
	suite.userKey.MustCmd(suite.otherPatch, "pr accept 7")

	t.Log("Create pr with user repo and admin can accept")
	suite.adminKey.MustCmd(nil, "repo create ai")
	suite.userKey.MustCmd(suite.patch, "pr create admin/ai")
	suite.adminKey.MustCmd(suite.otherPatch, "pr add --accept 8")

	t.Log("Create pr with default `bin` repo")
	actual, err := suite.userKey.Cmd(suite.patch, "pr create")
	bail(err)
	snaps.MatchSnapshot(t, actual)

	t.Log("Snapshot test ls command")
	actual, err = suite.userKey.Cmd(nil, "pr ls")
	bail(err)
	snaps.MatchSnapshot(t, actual)

	t.Log("Snapshot test logs command")
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
