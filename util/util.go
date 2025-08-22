package util

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

func CreateTmpDir() string {
	tmp, err := os.MkdirTemp(os.TempDir(), "git-pr*")
	if err != nil {
		panic(err)
	}
	return tmp
}

func CreateCfgFile(dataDir, cfgTmpl string, adminKey UserSSH) string {
	cfgPath := filepath.Join(dataDir, "git-pr.toml")
	cfgFi, err := os.Create(cfgPath)
	if err != nil {
		panic(err)
	}
	_, _ = fmt.Fprintf(cfgFi, cfgTmpl, dataDir, adminKey.Public())
	_ = cfgFi.Close()
	return cfgPath
}

type UserSSH struct {
	username string
	signer   ssh.Signer
}

func NewUserSSH(username string, signer ssh.Signer) *UserSSH {
	return &UserSSH{
		username: username,
		signer:   signer,
	}
}

func (s UserSSH) Public() string {
	pubkey := s.signer.PublicKey()
	return string(ssh.MarshalAuthorizedKey(pubkey))
}

func (s UserSSH) MustCmd(patch []byte, cmd string) string {
	res, err := s.Cmd(patch, cmd)
	if err != nil {
		panic(err)
	}
	return res
}

func (s UserSSH) Cmd(patch []byte, cmd string) (string, error) {
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
		return "", err
	}
	defer func() {
		_ = client.Close()
	}()

	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer func() {
		_ = session.Close()
	}()

	stdinPipe, err := session.StdinPipe()
	if err != nil {
		return "", err
	}

	stdoutPipe, err := session.StdoutPipe()
	if err != nil {
		return "", err
	}

	if err := session.Start(cmd); err != nil {
		return "", err
	}

	if patch != nil {
		_, err = stdinPipe.Write(patch)
		if err != nil {
			return "", err
		}
	}

	_ = stdinPipe.Close()

	if err := session.Wait(); err != nil {
		return "", err
	}

	buf := new(strings.Builder)
	_, err = io.Copy(buf, stdoutPipe)
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}

func GenerateKeys() (UserSSH, UserSSH) {
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

	return UserSSH{
			username: "admin",
			signer:   adminSigner,
		}, UserSSH{
			username: "contributor",
			signer:   userSigner,
		}
}
