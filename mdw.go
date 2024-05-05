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
		wish.Fatalln(sesh, err)
	}
}

func flagSet(sesh ssh.Session, cmdName string) *flag.FlagSet {
	cmd := flag.NewFlagSet(cmdName, flag.ContinueOnError)
	cmd.SetOutput(sesh)
	return cmd
}

type GitPatchRequest interface {
	GetPatchesByPrID(prID int64) ([]*Patch, error)
	SubmitPatch(pubkey string, prID int64, patch io.Reader) (*Patch, error)
	SubmitPatchRequest(pubkey string, repoName string, patches io.Reader) (*PatchRequest, error)
}

type PrCmd struct {
	Backend *Backend
}

var _ GitPatchRequest = PrCmd{}
var _ GitPatchRequest = (*PrCmd)(nil)

func (pr PrCmd) GetPatchesByPrID(prID int64) ([]*Patch, error) {
	patches := []*Patch{}
	err := pr.Backend.DB.Select(
		&patches,
		"SELECT * FROM patches WHERE patch_request_id=?",
		prID,
	)
	if err != nil {
		return patches, err
	}
	if len(patches) == 0 {
		return patches, fmt.Errorf("no patches found for Patch Request ID: %d", prID)
	}
	return patches, nil
}

func (cmd PrCmd) SubmitPatch(pubkey string, prID int64, patch io.Reader) (*Patch, error) {
	pr := PatchRequest{}
	err := cmd.Backend.DB.Get(&pr, "SELECT * FROM patch_requests WHERE id=?", prID)
	if err != nil {
		return nil, err
	}
	if pr.ID == 0 {
		return nil, fmt.Errorf("patch request (ID: %d) does not exist", prID)
	}

	review := false
	if pr.Pubkey != pubkey {
		review = true
	}

	// need to read io.Reader from session twice
	var buf bytes.Buffer
	tee := io.TeeReader(patch, &buf)

	_, preamble, err := gitdiff.Parse(tee)
	if err != nil {
		return nil, err
	}
	header, err := gitdiff.ParsePatchHeader(preamble)
	if err != nil {
		return nil, err
	}

	patchID := 0
	row := cmd.Backend.DB.QueryRow(
		"INSERT INTO patches (pubkey, patch_request_id, author_name, author_email, title, body, commit_sha, commit_date, review, raw_text) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id",
		pubkey,
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
	err = row.Scan(&patchID)
	if err != nil {
		return nil, err
	}

	var patchRec Patch
	err = cmd.Backend.DB.Get(&patchRec, "SELECT * FROM patches WHERE id=?")
	return &patchRec, err
}

func (cmd PrCmd) SubmitPatchRequest(pubkey string, repoName string, patches io.Reader) (*PatchRequest, error) {
	err := git.EnsureWithin(cmd.Backend.ReposDir(), cmd.Backend.RepoName(repoName))
	if err != nil {
		return nil, err
	}
	_, err = os.Stat(filepath.Join(cmd.Backend.ReposDir(), cmd.Backend.RepoName(repoName)))
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("repo does not exist: %s", repoName)
	}

	// need to read io.Reader from session twice
	var buf bytes.Buffer
	tee := io.TeeReader(patches, &buf)

	_, preamble, err := gitdiff.Parse(tee)
	if err != nil {
		return nil, err
	}
	header, err := gitdiff.ParsePatchHeader(preamble)
	if err != nil {
		return nil, err
	}
	prName := header.Title
	prDesc := header.Body

	var prID int64
	row := cmd.Backend.DB.QueryRow(
		"INSERT INTO patch_requests (pubkey, repo_id, name, text, updated_at) VALUES(?, ?, ?, ?, ?) RETURNING id",
		pubkey,
		repoName,
		prName,
		prDesc,
		time.Now(),
	)
	err = row.Scan(&prID)
	if err != nil {
		return nil, err
	}
	if prID == 0 {
		return nil, fmt.Errorf("could not create patch request")
	}

	_, err = cmd.Backend.DB.Exec(
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
	if err != nil {
		return nil, err
	}

	var pr PatchRequest
	err = cmd.Backend.DB.Get(&pr, "SELECT * FROM patch_requests WHERE id=?", prID)
	return &pr, err
}

func GitPatchRequestMiddleware(be *Backend, pr GitPatchRequest) wish.Middleware {
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
				try(sesh, err)

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

				if *out {
					patches, err := pr.GetPatchesByPrID(prID)
					try(sesh, err)

					if len(patches) == 0 {
						wish.Println(sesh, patches[0].RawText)
						return
					}

					for _, patch := range patches {
						wish.Printf(sesh, "%s\n\n\n", patch.RawText)
					}
				} else if prID != 0 {
					patch, err := pr.SubmitPatch(pubkey, prID, sesh)
					if err != nil {
						wish.Fatalln(sesh, err)
						return
					}
					wish.Printf(sesh, "Patch submitted! (ID:%d)\n", patch.ID)
				} else if subCmd == "ls" {
					wish.Println(sesh, "list all patch requests")
				} else if repoName != "" {
					request, err := pr.SubmitPatchRequest(pubkey, repoName, sesh)
					if err != nil {
						wish.Fatalln(sesh, err)
					}
					wish.Printf(sesh, "Patch Request submitted! (ID:%d)\n", request.ID)
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
