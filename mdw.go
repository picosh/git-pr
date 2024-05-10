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

func gitServiceCommands(sesh ssh.Session, be *Backend, cmd, repoName string) error {
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
	GetRepos() ([]string, error)
	SubmitPatchRequest(pubkey string, repoID string, patches io.Reader) (*PatchRequest, error)
	SubmitPatch(pubkey string, prID int64, review bool, patch io.Reader) (*Patch, error)
	GetPatchRequests() ([]*PatchRequest, error)
	GetPatchesByPrID(prID int64) ([]*Patch, error)
	UpdatePatchRequest(prID int64, status string) error
}

type PrCmd struct {
	Backend *Backend
}

var _ GitPatchRequest = PrCmd{}
var _ GitPatchRequest = (*PrCmd)(nil)

func (pr PrCmd) GetRepos() ([]string, error) {
	repos := []string{}
	entries, err := os.ReadDir(pr.Backend.ReposDir())
	if err != nil {
		return repos, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			repos = append(repos, entry.Name())
		}
	}
	return repos, nil
}

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

func (cmd PrCmd) SubmitPatch(pubkey string, prID int64, review bool, patch io.Reader) (*Patch, error) {
	pr := PatchRequest{}
	err := cmd.Backend.DB.Get(&pr, "SELECT * FROM patch_requests WHERE id=?", prID)
	if err != nil {
		return nil, err
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
	err = cmd.Backend.DB.Get(&patchRec, "SELECT * FROM patches WHERE id=?", patchID)
	return &patchRec, err
}

func (cmd PrCmd) SubmitPatchRequest(pubkey string, repoID string, patches io.Reader) (*PatchRequest, error) {
	err := git.EnsureWithin(cmd.Backend.ReposDir(), repoID)
	if err != nil {
		return nil, err
	}
	loc := filepath.Join(cmd.Backend.ReposDir(), repoID)
	_, err = os.Stat(loc)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("repo does not exist: %s", loc)
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
		"INSERT INTO patch_requests (pubkey, repo_id, name, text, status, updated_at) VALUES(?, ?, ?, ?, ?, ?) RETURNING id",
		pubkey,
		repoID,
		prName,
		prDesc,
		"open",
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
			cmd := "help"
			if len(args) > 0 {
				cmd = args[0]
			}

			if cmd == "git-receive-pack" || cmd == "git-upload-pack" {
				repoName := args[1]
				err := gitServiceCommands(sesh, be, cmd, repoName)
				if err != nil {
					wish.Fatalln(sesh, err)
					return
				}
			} else if cmd == "help" {
				wish.Println(sesh, "commands: [help, pr, ls, git-receive-pack, git-upload-pack]")
			} else if cmd == "ls" {
				repos, err := pr.GetRepos()
				if err != nil {
					wish.Fatalln(sesh, err)
					return
				}
				wish.Printf(sesh, "Name\tDir\n")
				for _, repo := range repos {
					wish.Printf(
						sesh,
						"%s\t%s\n",
						utils.SanitizeRepo(repo),
						filepath.Join(be.ReposDir(), repo),
					)
				}
			} else if cmd == "pr" {
				prCmd := flagSet(sesh, "pr")
				out := prCmd.Bool("stdout", false, "print patchset to stdout")
				accept := prCmd.Bool("accept", false, "mark patch request as accepted")
				closed := prCmd.Bool("close", false, "mark patch request as closed")
				review := prCmd.Bool("review", false, "mark patch request as reviewed")
				stats := prCmd.Bool("stats", false, "return summary instead of patches")

				if len(args) < 2 {
					wish.Fatalln(sesh, "must provide repo name or patch request ID")
					return
				}

				var err error
				err = prCmd.Parse(args[2:])
				if err != nil {
					wish.Fatalln(sesh, err)
					return
				}
				subCmd := strings.TrimSpace(args[1])

				repoID := ""
				var prID int64
				// figure out subcommand based on what was passed in
				if subCmd == "ls" {
					// skip proccessing
				} else if isNumRe.MatchString(subCmd) {
					// we probably have a patch request id
					prID, err = strconv.ParseInt(subCmd, 10, 64)
					if err != nil {
						wish.Fatalln(sesh, err)
						return
					}
					subCmd = "patchRequest"
				} else {
					// we probably have a repo name
					repoID = be.RepoID(subCmd)
					subCmd = "submitPatchRequest"
				}

				if subCmd == "ls" {
					prs, err := pr.GetPatchRequests()
					if err != nil {
						wish.Fatalln(sesh, err)
						return
					}
					wish.Printf(sesh, "Name\tID\n")
					for _, req := range prs {
						wish.Printf(sesh, "%s\t%d\n", req.Name, req.ID)
					}
				} else if subCmd == "submitPatchRequest" {
					request, err := pr.SubmitPatchRequest(pubkey, repoID, sesh)
					if err != nil {
						wish.Fatalln(sesh, err)
						return
					}
					wish.Printf(sesh, "Patch Request submitted! Use the ID for interacting with this Patch Request.\nID\tName\n%d\t%s\n", request.ID, request.Name)
				} else if subCmd == "patchRequest" {
					if *out {
						patches, err := pr.GetPatchesByPrID(prID)
						if err != nil {
							wish.Fatalln(sesh, err)
							return
						}

						if *stats {
							for _, patch := range patches {
								reviewTxt := ""
								if patch.Review {
									reviewTxt = "[R]"
								}
								wish.Printf(
									sesh,
									"%s %s\n%s <%s>\n%s\n%s\n---\n",
									patch.Title,
									reviewTxt,
									patch.AuthorName,
									patch.AuthorEmail,
									patch.CommitDate.Format(time.RFC3339Nano),
									patch.Body,
								)
							}
						} else {
							if len(patches) == 1 {
								wish.Println(sesh, patches[0].RawText)
								return
							}

							for _, patch := range patches {
								wish.Printf(sesh, "%s\n\n\n", patch.RawText)
							}
						}

					} else if *accept {
						if !be.IsAdmin(sesh.PublicKey()) {
							wish.Fatalln(sesh, "must be admin to accept PR")
							return
						}
						err := pr.UpdatePatchRequest(prID, "accept")
						if err != nil {
							wish.Fatalln(sesh, err)
							return
						}
					} else if *closed {
						if !be.IsAdmin(sesh.PublicKey()) {
							wish.Fatalln(sesh, "must be admin to close PR")
							return
						}
						err := pr.UpdatePatchRequest(prID, "close")
						if err != nil {
							wish.Fatalln(sesh, err)
							return
						}
					} else {
						isAdmin := be.IsAdmin(sesh.PublicKey())
						var req PatchRequest
						err = be.DB.Get(&req, "SELECT * FROM patch_requests WHERE id=?", prID)
						if err != nil {
							wish.Fatalln(sesh, err)
							return
						}
						isPrOwner := req.Pubkey == be.Pubkey(sesh.PublicKey())
						if !isAdmin && !isPrOwner {
							wish.Fatalln(sesh, "unauthorized, you are not the owner of this Patch Request")
							return
						}

						patch, err := pr.SubmitPatch(pubkey, prID, *review, sesh)
						if err != nil {
							wish.Fatalln(sesh, err)
							return
						}
						if *review {
							err = pr.UpdatePatchRequest(prID, "review")
							if err != nil {
								wish.Fatalln(sesh, err)
								return
							}
						}
						wish.Printf(sesh, "Patch submitted! (ID:%d)\n", patch.ID)
					}
				}

				return
			} else {
				next(sesh)
				return
			}
		}
	}
}
