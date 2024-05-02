package git

import (
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
	"github.com/charmbracelet/soft-serve/pkg/git"
	"github.com/charmbracelet/soft-serve/pkg/utils"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
)

func gitServiceCommands(sesh ssh.Session, be *Backend, cmd, repo string) error {
	name := utils.SanitizeRepo(repo)
	// git bare repositories should end in ".git"
	// https://git-scm.com/docs/gitrepository-layout
	repoName := name + ".git"
	reposDir := be.ReposDir()
	err := git.EnsureWithin(reposDir, repoName)
	if err != nil {
		return err
	}
	repoPath := filepath.Join(reposDir, repoName)
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

func try(sesh ssh.Session, err error) {
	if err != nil {
		wish.Fatal(sesh, err)
	}
}

func GitServerMiddleware(be *Backend) wish.Middleware {
	return func(next ssh.Handler) ssh.Handler {
		return func(sesh ssh.Session) {
			pubkey := be.Pubkey(sesh.PublicKey())
			args := sesh.Command()
			cmd := args[0]

			if cmd == "git-receive-pack" || cmd == "git-upload-pack" {
				repoName := args[1]
				err := gitServiceCommands(sesh, be, cmd, repoName)
				try(sesh, err)
			} else if cmd == "help" {
				wish.Println(sesh, "commands: [help, git-receive-pack, git-upload-pack]")
			} else if cmd == "pr" {
				if len(args) < 2 {
					wish.Fatal(sesh, "must provide repo name")
					return
				}
				repoName := utils.SanitizeRepo(args[1])
				err := git.EnsureWithin(be.ReposDir(), be.RepoName(repoName))
				try(sesh, err)

				// need to read io.Reader from session twice
				var buf bytes.Buffer
				tee := io.TeeReader(sesh, &buf)

				_, preamble, err := gitdiff.Parse(tee)
				try(sesh, err)
				header, err := gitdiff.ParsePatchHeader(preamble)
				try(sesh, err)
				prName := header.Title
				prDesc := header.Body

				var prID int64
				row := be.DB.QueryRow(
					"INSERT INTO patch_requests (pubkey, repo_id, name, text, updated_at) VALUES(?, ?, ?, ?, ?) RETURNING id",
					pubkey,
					repoName,
					prName,
					prDesc,
					time.Now(),
				)
				row.Scan(&prID)
				if prID == 0 {
					wish.Fatal(sesh, "could not create patch request")
					return
				}
				try(sesh, err)

				_, err = be.DB.Exec(
					"INSERT INTO patches (pubkey, patch_request_id, author_name, author_email, title, body, commit_sha, commit_date, raw_text) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
					pubkey,
					prID,
					header.Author.Name,
					header.Author.Email,
					header.Title,
					header.Body,
					header.SHA,
					header.CommitterDate,
					buf.String(),
				)
				try(sesh, err)

				wish.Printf(
					sesh,
					"Create Patch Request!\nID: %d\nTitle: %s\n",
					prID,
					prName,
				)

				return
			} else {
				fmt.Println("made it here")
				next(sesh)
				return
			}
		}
	}
}
