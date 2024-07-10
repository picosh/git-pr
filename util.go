package git

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"

	"github.com/charmbracelet/ssh"
)

var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

// https://stackoverflow.com/a/22892986
func randSeq(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func truncateSha(sha string) string {
	return sha[:7]
}

func getAuthorizedKeys(pubkeys []string) ([]ssh.PublicKey, error) {
	keys := []ssh.PublicKey{}
	for _, pubkey := range pubkeys {
		if strings.TrimSpace(pubkey) == "" {
			continue
		}
		if strings.HasPrefix(pubkey, "#") {
			continue
		}
		upk, _, _, _, err := ssh.ParseAuthorizedKey([]byte(pubkey))
		if err != nil {
			return keys, err
		}
		keys = append(keys, upk)
	}

	return keys, nil
}

type Ranger struct {
	Left  int
	Right int
}

func parseRange(rnge string, sliceLen int) (*Ranger, error) {
	items := strings.Split(rnge, ":")
	left := 0
	var err error
	if items[0] != "" {
		left, err = strconv.Atoi(items[0])
		if err != nil {
			return nil, fmt.Errorf("first value before `:` must provide number")
		}
	}

	if left < 0 {
		return nil, fmt.Errorf("first value must be >= 0")
	}

	if left >= sliceLen {
		return nil, fmt.Errorf("first value must be less than number of patches")
	}

	if len(items) == 1 {
		return &Ranger{
			Left:  left,
			Right: left,
		}, nil
	}

	if items[1] == "" {
		return &Ranger{Left: left, Right: sliceLen - 1}, nil
	}

	right, err := strconv.Atoi(items[1])
	if err != nil {
		return nil, fmt.Errorf("second value after `:` must provide number")
	}

	if left > right {
		return nil, fmt.Errorf("second value must be greater than first value")
	}

	if right >= sliceLen {
		return nil, fmt.Errorf("second value must be less than number of patches")
	}

	return &Ranger{
		Left:  left,
		Right: right,
	}, nil
}

func filterPatches(ranger *Ranger, patches []*Patch) []*Patch {
	if ranger.Left == ranger.Right {
		return []*Patch{patches[ranger.Left]}
	}
	return patches[ranger.Left:ranger.Right]
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
