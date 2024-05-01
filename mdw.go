package git

import (
	"fmt"
	"path/filepath"

	ssgit "github.com/charmbracelet/soft-serve/git"
	"github.com/charmbracelet/soft-serve/pkg/git"
	"github.com/charmbracelet/soft-serve/pkg/utils"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
)

func gitServiceCommands(sesh ssh.Session, cfg *GitCfg, cmd, repo string) error {
	name := utils.SanitizeRepo(repo)
	// git bare repositories should end in ".git"
	// https://git-scm.com/docs/gitrepository-layout
	repoDir := name + ".git"
	reposDir := filepath.Join(cfg.DataPath, "repos")
	err := git.EnsureWithin(reposDir, repoDir)
	if err != nil {
		return err
	}
	repoPath := filepath.Join(reposDir, repoDir)
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
}

func createRepo(cfg *GitCfg, rawName string) (*Repo, error) {
	name := utils.SanitizeRepo(rawName)
	if err := utils.ValidateRepo(name); err != nil {
		return nil, err
	}
	reposDir := filepath.Join(cfg.DataPath, "repos")

	repo := name + ".git"
	rp := filepath.Join(reposDir, repo)
	_, err := ssgit.Init(rp, true)
	if err != nil {
		return nil, err
	}
}

func GitServerMiddleware(cfg *GitCfg, dbh *DB) wish.Middleware {
	return func(next ssh.Handler) ssh.Handler {
		return func(sesh ssh.Session) {
			args := sesh.Command()
			cmd := args[0]
			fmt.Println(cmd)

			if cmd == "git-receive-pack" || cmd == "git-upload-pack" {
				repoName := args[1]
				err := gitServiceCommands(sesh, cfg, cmd, repoName)
				if err != nil {
					wish.Fatal(sesh, err.Error())
					return
				}
			} else if cmd == "help" {
				wish.Println(sesh, "commands: [help, git-receive-pack, git-upload-pack]")
			} else if cmd == "pr" {
				repoName := args[1]
				fmt.Println(repoName)
				// dbpool.GetRepoByName(repoName)
				// pr, err := dbpool.InsertPatchRequest(userID, repoID, name)
				// dbpool.InsertPatches(userID, pr.ID, patches)
				// id := fmt.Sprintf("%s/%s", repoName, pr.ID)
				// wish.Printf("Patch Request ID: %s", id)
			} else {
				fmt.Println("made it here")
				next(sesh)
				return
			}
		}
	}
}
