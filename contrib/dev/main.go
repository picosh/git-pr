package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/picosh/git-pr"
	"github.com/picosh/git-pr/fixtures"
	"golang.org/x/crypto/ssh"
)

func main() {
	cleanupFlag := flag.Bool("cleanup", true, "Clean up tmp dir after quitting (default: true)")
	flag.Parse()

	tmp, err := os.MkdirTemp(os.TempDir(), "git-pr*")
	if err != nil {
		panic(err)
	}
	defer func() {
		if *cleanupFlag {
			os.RemoveAll(tmp)
		}
	}()
	fmt.Println(tmp)

	adminKey, userKey := generateKeys()

	cfgPath := filepath.Join(tmp, "git-pr.toml")
	cfgFi, err := os.Create(cfgPath)
	if err != nil {
		panic(err)
	}
	cfgFi.WriteString(fmt.Sprintf(cfgTmpl, tmp, adminKey.public()))
	cfgFi.Close()

	opts := &slog.HandlerOptions{
		AddSource: true,
	}
	logger := slog.New(
		slog.NewTextHandler(os.Stdout, opts),
	)
	cfg := git.NewGitCfg(cfgPath, logger)
	go git.GitSshServer(cfg)
	time.Sleep(time.Second)
	go git.StartWebServer(cfg)

	// Hack to wait for startup
	time.Sleep(time.Second)

	patch, err := fixtures.Fixtures.ReadFile("single.patch")
	if err != nil {
		panic(err)
	}
	otherPatch, err := fixtures.Fixtures.ReadFile("with-cover.patch")
	if err != nil {
		panic(err)
	}

	// Accepted patch
	userKey.cmd(patch, "pr create test")
	userKey.cmd(nil, "pr edit 1 Accepted patch")
	adminKey.cmd(nil, "pr accept 1")

	// Closed patch (admin)
	userKey.cmd(patch, "pr create test")
	userKey.cmd(nil, "pr edit 2 Closed patch (admin)")
	adminKey.cmd(nil, "pr close 2")

	// Closed patch (contributor)
	userKey.cmd(patch, "pr create test")
	userKey.cmd(nil, "pr edit 3 Closed patch (contributor)")
	userKey.cmd(nil, "pr close 3")

	// Reviewed patch
	userKey.cmd(patch, "pr create test")
	userKey.cmd(nil, "pr edit 4 Reviewed patch")
	adminKey.cmd(otherPatch, "pr add --review 4")

	// Accepted patch with review
	userKey.cmd(patch, "pr create test")
	userKey.cmd(nil, "pr edit 5 Accepted patch with review")
	adminKey.cmd(otherPatch, "pr add --accept 5")

	// Closed patch with review
	userKey.cmd(patch, "pr create test")
	userKey.cmd(nil, "pr edit 6 Closed patch with review")
	adminKey.cmd(otherPatch, "pr add --close 6")

	fmt.Println("time to do some testing...")
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, os.Kill)
	<-ch
}

type sshKey struct {
	username string
	signer   ssh.Signer
}

func (s sshKey) public() string {
	pubkey := s.signer.PublicKey()
	return string(ssh.MarshalAuthorizedKey(pubkey))
}

func (s sshKey) cmd(patch []byte, cmd string) {
	host := "localhost:2222"

	config := &ssh.ClientConfig{
		User: s.username,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(s.signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	client, err := ssh.Dial("tcp", host, config)
	if err != nil {
		panic(err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		panic(err)
	}
	defer session.Close()

	stdinPipe, err := session.StdinPipe()
	if err != nil {
		panic(err)
	}

	if err := session.Start(cmd); err != nil {
		panic(err)
	}

	if patch != nil {
		_, err = stdinPipe.Write(patch)
		if err != nil {
			panic(err)
		}
	}

	stdinPipe.Close()

	if err := session.Wait(); err != nil {
		panic(err)
	}
}

func generateKeys() (sshKey, sshKey) {
	_, adminKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}

	adminSigner, err := ssh.NewSignerFromKey(adminKey)
	if err != nil {
		panic(err)
	}

	_, userKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}

	userSigner, err := ssh.NewSignerFromKey(userKey)
	if err != nil {
		panic(err)
	}

	return sshKey{
			username: "admin",
			signer:   adminSigner,
		}, sshKey{
			username: "contributor",
			signer:   userSigner,
		}
}

// args: tmpdir, adminKey
var cfgTmpl = `# url is used for help commands, exclude protocol
url = "localhost"
# where we store the sqlite db, this toml file, git repos, and ssh host keys
data_dir = %q
# this gives users the ability to submit reviews and other admin permissions
admins = [%q]
# set datetime format for our clients
time_format = "01/02/2006 15:04:05 07:00"

# add as many repos as you want
[[repo]]
id = "test"
clone_addr = "https://github.com/picosh/test.git"
desc = "Test repo"`
