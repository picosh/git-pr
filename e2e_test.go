package git

import (
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

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
	// Hack to wait for startup
	time.Sleep(time.Millisecond * 100)
	// Accepted patch
	suite.userKey.MustCmd(suite.patch, "pr create test")
	suite.userKey.MustCmd(nil, "pr edit 1 Accepted patch")
	suite.adminKey.MustCmd(nil, "pr accept 1")

	// Closed patch (admin)
	suite.userKey.MustCmd(suite.patch, "pr create test")
	suite.userKey.MustCmd(nil, "pr edit 2 Closed patch (admin)")
	suite.adminKey.MustCmd(nil, "pr close 2")

	// Closed patch (contributor)
	suite.userKey.MustCmd(suite.patch, "pr create test")
	suite.userKey.MustCmd(nil, "pr edit 3 Closed patch (contributor)")
	suite.userKey.MustCmd(nil, "pr close 3")

	// Reviewed patch
	suite.userKey.MustCmd(suite.patch, "pr create test")
	suite.userKey.MustCmd(nil, "pr edit 4 Reviewed patch")
	suite.adminKey.MustCmd(suite.otherPatch, "pr add --review 4")

	// Accepted patch with review
	suite.userKey.MustCmd(suite.patch, "pr create test")
	suite.userKey.MustCmd(nil, "pr edit 5 Accepted patch with review")
	suite.adminKey.MustCmd(suite.otherPatch, "pr add --accept 5")

	// Closed patch with review
	suite.userKey.MustCmd(suite.patch, "pr create test")
	suite.userKey.MustCmd(nil, "pr edit 6 Closed patch with review")
	suite.adminKey.MustCmd(suite.otherPatch, "pr add --close 6")

	actual, err := suite.userKey.Cmd(nil, "pr ls")
	bail(err)
	if strings.TrimSpace(actual) != prLsExpected {
		t.Errorf("\nexpected:\n%s\n\nactual:\n%s\n", prLsExpected, actual)
	}
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

var prLsExpected = `ID RepoID           Name                       Status     Patchsets User        Date
1  contributor/test Accepted patch             [accepted] 1         contributor 
2  contributor/test Closed patch (admin)       [closed]   1         contributor 
3  contributor/test Closed patch (contributor) [closed]   1         contributor 
4  contributor/test Reviewed patch             [reviewed] 2         contributor 
5  contributor/test Accepted patch with review [accepted] 2         contributor 
6  contributor/test Closed patch with review   [closed]   2         contributor`
