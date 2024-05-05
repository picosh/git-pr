package git

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
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

func flagSet(sesh ssh.Session, cmdName string) *flag.FlagSet {
	cmd := flag.NewFlagSet(cmdName, flag.ContinueOnError)
	cmd.SetOutput(sesh)
	return cmd
}

type PrCmd struct {
	Session ssh.Session
	Backend *Backend
	Repo    string
	Pubkey  string
}

func (pr *PrCmd) PrintPatches(prID int64) {
	patches := []*Patch{}
	pr.Backend.DB.Select(
		&patches,
		"SELECT * FROM patches WHERE patch_request_id=?",
		prID,
	)
	if len(patches) == 0 {
		wish.Printf(pr.Session, "no patches found for Patch Request ID: %d\n", prID)
		return
	}

	if len(patches) == 1 {
		wish.Println(pr.Session, patches[0].RawText)
		return
	}

	for _, patch := range patches {
		wish.Printf(pr.Session, "%s\n\n\n", patch.RawText)
	}
}

func (cmd *PrCmd) SubmitPatch(prID int64) {
	pr := PatchRequest{}
	cmd.Backend.DB.Get(&pr, "SELECT * FROM patch_requests WHERE id=?", prID)
	if pr.ID == 0 {
		wish.Fatalln(cmd.Session, "patch request does not exist")
		return
	}

	review := false
	if pr.Pubkey != cmd.Pubkey {
		review = true
	}

	// need to read io.Reader from session twice
	var buf bytes.Buffer
	tee := io.TeeReader(cmd.Session, &buf)

	_, preamble, err := gitdiff.Parse(tee)
	try(cmd.Session, err)
	header, err := gitdiff.ParsePatchHeader(preamble)
	try(cmd.Session, err)

	_, err = cmd.Backend.DB.Exec(
		"INSERT INTO patches (pubkey, patch_request_id, author_name, author_email, title, body, commit_sha, commit_date, review, raw_text) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		cmd.Pubkey,
		prID,
		header.Author.Name,
		header.Author.Email,
		header.Title,
		header.Body,
		header.SHA,
		header.CommitterDate,
		review,
		buf.String(),
	)
	try(cmd.Session, err)

	wish.Printf(cmd.Session, "submitted review!\n")
}

func (cmd *PrCmd) SubmitPatchRequest(repoName string) {
	err := git.EnsureWithin(cmd.Backend.ReposDir(), cmd.Backend.RepoName(repoName))
	try(cmd.Session, err)
	_, err = os.Stat(filepath.Join(cmd.Backend.ReposDir(), cmd.Backend.RepoName(repoName)))
	if os.IsNotExist(err) {
		wish.Fatalln(cmd.Session, "repo does not exist")
		return
	}

	// need to read io.Reader from session twice
	var buf bytes.Buffer
	tee := io.TeeReader(cmd.Session, &buf)

	_, preamble, err := gitdiff.Parse(tee)
	try(cmd.Session, err)
	header, err := gitdiff.ParsePatchHeader(preamble)
	try(cmd.Session, err)
	prName := header.Title
	prDesc := header.Body

	var prID int64
	row := cmd.Backend.DB.QueryRow(
		"INSERT INTO patch_requests (pubkey, repo_id, name, text, updated_at) VALUES(?, ?, ?, ?, ?) RETURNING id",
		cmd.Pubkey,
		repoName,
		prName,
		prDesc,
		time.Now(),
	)
	row.Scan(&prID)
	if prID == 0 {
		wish.Fatal(cmd.Session, "could not create patch request")
		return
	}
	try(cmd.Session, err)

	_, err = cmd.Backend.DB.Exec(
		"INSERT INTO patches (pubkey, patch_request_id, author_name, author_email, title, body, commit_sha, commit_date, raw_text) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		cmd.Pubkey,
		prID,
		header.Author.Name,
		header.Author.Email,
		header.Title,
		header.Body,
		header.SHA,
		header.CommitterDate,
		buf.String(),
	)
	try(cmd.Session, err)

	wish.Printf(
		cmd.Session,
		"created patch request!\nID: %d\nTitle: %s\n",
		prID,
		prName,
	)
}

func GitPatchRequestMiddleware(be *Backend) wish.Middleware {
	isNumRe := regexp.MustCompile(`^\d+$`)

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
				wish.Println(sesh, "commands: [help, pr, ls, git-receive-pack, git-upload-pack]")
			} else if cmd == "ls" {
				entries, err := os.ReadDir(be.ReposDir())
				if err != nil {
					wish.Fatal(sesh, err)
				}

				for _, e := range entries {
					if e.IsDir() {
						wish.Println(sesh, utils.SanitizeRepo(e.Name()))
					}
				}
			} else if cmd == "pr" {
				// PATCH REQUEST STATUS:
				// APPROVED
				// CLOSED
				// REVIEWED

				// ssh git.sh ls
				// git format-patch --stdout | ssh git.sh pr test
				// git format-patch --stdout | ssh git.sh pr 123
				// ssh git.sh pr ls
				// ssh git.sh pr 123 --approve
				// ssh git.sh pr 123 --close
				// ssh git.sh pr 123 --stdout | git am -3
				// echo "here is a comment" | ssh git.sh pr 123 --comment

				prCmd := flagSet(sesh, "pr")
				subCmd := strings.TrimSpace(args[2])
				repoName := ""
				var prID int64
				var err error
				if isNumRe.MatchString(subCmd) {
					prID, err = strconv.ParseInt(args[2], 10, 64)
					try(sesh, err)
				} else {
					repoName = utils.SanitizeRepo(subCmd)
				}
				out := prCmd.Bool("stdout", false, "print patchset to stdout")

				pr := &PrCmd{
					Session: sesh,
					Backend: be,
					Pubkey:  pubkey,
				}

				if *out == true {
					pr.PrintPatches(prID)
				} else if prID != 0 {
					pr.SubmitPatch(prID)
				} else if subCmd == "ls" {
					wish.Println(sesh, "list all patch requests")
				} else if repoName != "" {
					pr.SubmitPatchRequest(repoName)
				}

				return
			} else {
				fmt.Println("made it here")
				next(sesh)
				return
			}
		}
	}
}
