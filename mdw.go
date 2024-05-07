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

// ssh git.sh ls
// git format-patch --stdout | ssh git.sh pr test
// git format-patch --stdout | ssh git.sh pr 123 --review
// ssh git.sh pr ls
// ssh git.sh pr 123 --stdout | git am -3
// ssh git.sh pr 123 --approve # or --close
// echo "here is a comment" | ssh git.sh pr 123 --comment

type GitPatchRequest interface {
	SubmitPatchRequest(pubkey string, repoName string, patches io.Reader) (*PatchRequest, error)
	SubmitPatch(pubkey string, prID int64, patch io.Reader, review bool) (*Patch, error)
	GetPatchRequests() ([]*PatchRequest, error)
	GetPatchesByPrID(prID int64) ([]*Patch, error)
	UpdatePatchRequest(prID int64, status string) error
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

func (cmd PrCmd) GetPatchRequests() ([]*PatchRequest, error) {
	prs := []*PatchRequest{}
	err := cmd.Backend.DB.Select(
		&prs,
		"SELECT * FROM patch_requests",
	)
	return prs, err
}

// status types: open, close, accept, review
func (cmd PrCmd) UpdatePatchRequest(prID int64, status string) error {
	_, err := cmd.Backend.DB.Exec(
		"UPDATE patch_requests SET status=? WHERE id=?", status, prID,
	)
	return err
}

func (cmd PrCmd) SubmitPatch(pubkey string, prID int64, patch io.Reader, review bool) (*Patch, error) {
	pr := PatchRequest{}
	err := cmd.Backend.DB.Get(&pr, "SELECT * FROM patch_requests WHERE id=?", prID)
	if err != nil {
		return nil, err
	}
	if pr.ID == 0 {
		return nil, fmt.Errorf("patch request (ID: %d) does not exist", prID)
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
				help := prCmd.Bool("help", false, "print patch request help")
				out := prCmd.Bool("stdout", false, "print patchset to stdout")
				accept := prCmd.Bool("accept", false, "mark patch request as accepted")
				closed := prCmd.Bool("close", false, "mark patch request as closed")
				review := prCmd.Bool("review", false, "mark patch request as reviewed")

				if *help {
					wish.Println(sesh, "commands: [pr ls, pr {id}]")
				} else if prID != 0 && *out {
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
					if *accept {
						if !be.IsAdmin(sesh.PublicKey()) {
							wish.Fatalln(sesh, "must be admin to accept PR")
							return
						}
						err := pr.UpdatePatchRequest(prID, "accept")
						try(sesh, err)
					} else if *closed {
						if !be.IsAdmin(sesh.PublicKey()) {
							wish.Fatalln(sesh, "must be admin to close PR")
							return
						}
						err := pr.UpdatePatchRequest(prID, "close")
						try(sesh, err)
					} else {
						rv := *review
						isAdmin := be.IsAdmin(sesh.PublicKey())
						if !isAdmin {
							rv = false
						}
						var req PatchRequest
						err = be.DB.Get(&req, "SELECT * FROM patch_requests WHERE id=?", prID)
						try(sesh, err)
						isOwner := req.Pubkey != be.Pubkey(sesh.PublicKey())
						if !isAdmin || isOwner {
							wish.Fatalln(sesh, "unauthorized, you are not the owner of this Patch Request")
							return
						}

						patch, err := pr.SubmitPatch(pubkey, prID, sesh, rv)
						if err != nil {
							wish.Fatalln(sesh, err)
							return
						}
						if *review {
							err = pr.UpdatePatchRequest(prID, "review")
							try(sesh, err)
						}
						wish.Printf(sesh, "Patch submitted! (ID:%d)\n", patch.ID)
					}
				} else if subCmd == "ls" {
					prs, err := pr.GetPatchRequests()
					try(sesh, err)
					wish.Printf(sesh, "Name\tID\n")
					for _, req := range prs {
						wish.Printf(sesh, "%s\t%d\n", req.Name, req.ID)
					}
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
