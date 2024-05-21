package git

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/ssh"
)

func truncateSha(sha string) string {
	return sha[:7]
}

func getAuthorizedKeys(path string) ([]ssh.PublicKey, error) {
	keys := []ssh.PublicKey{}
	f, err := os.Open(path)
	if err != nil {
		return keys, err
	}
	defer f.Close() // nolint: errcheck

	rd := bufio.NewReader(f)
	for {
		line, _, err := rd.ReadLine()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return keys, err
		}
		if strings.TrimSpace(string(line)) == "" {
			continue
		}
		if bytes.HasPrefix(line, []byte{'#'}) {
			continue
		}
		upk, _, _, _, err := ssh.ParseAuthorizedKey(line)
		if err != nil {
			return keys, err
		}
		keys = append(keys, upk)
	}

	return keys, nil
}

/* func gitServiceCommands(sesh ssh.Session, be *Backend, cmd, repoName string) error {
	name := utils.SanitizeRepo(repoName)
	// git bare repositories should end in ".git"
	// https://git-scm.com/docs/gitrepository-layout
	repoID := be.RepoID(name)
	reposDir := be.ReposDir()
	err := git.EnsureWithin(reposDir, repoID)
	if err != nil {
		return err
	}
	repoPath := filepath.Join(reposDir, repoID)
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
			return err
		}
	} else if cmd == "git-upload-pack" {
		err := git.UploadPack(sesh.Context(), serviceCmd)
		if err != nil {
			return err
		}
	}

	return nil
} */
