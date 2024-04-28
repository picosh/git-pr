package git

import (
	"fmt"
	"path/filepath"

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

func GitServerMiddleware(cfg *GitCfg) wish.Middleware {
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
			} else {
				fmt.Println("made it here")
				next(sesh)
				return
			}
		}
	}
}
