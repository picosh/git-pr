package git

import (
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

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

func NewTabWriter(out io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(out, 0, 0, 1, ' ', tabwriter.TabIndent)
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
				writer := NewTabWriter(sesh)
				fmt.Fprintln(writer, "Name\tDir")
				for _, repo := range repos {
					fmt.Fprintf(
						writer,
						"%s\t%s\n",
						utils.SanitizeRepo(repo),
						filepath.Join(be.ReposDir(), repo),
					)
				}
				writer.Flush()
			} else if cmd == "pr" {
				/*
					ssh git.sh ls
					ssh git.sh pr ls
					git format-patch -1 HEAD~1 --stdout | ssh git.sh pr create
					ssh git.sh pr print 1
					ssh git.sh pr print 1 --summary
					ssh git.sh pr print 1 --ls
					ssh git.sh pr accept 1
					ssh git.sh pr close 1
					git format-patch -1 HEAD~1 --stdout | ssh git.sh pr review 1
					echo "my feedback" | ssh git.sh pr comment 1
				*/
				prCmd := flagSet(sesh, "pr")
				out := prCmd.Bool("stdout", false, "print patchset to stdout")
				accept := prCmd.Bool("accept", false, "mark patch request as accepted")
				closed := prCmd.Bool("close", false, "mark patch request as closed")
				review := prCmd.Bool("review", false, "mark patch request as reviewed")
				stats := prCmd.Bool("stats", false, "return summary instead of patches")
				prLs := prCmd.Bool("ls", false, "return list of patches")

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

					writer := NewTabWriter(sesh)
					fmt.Fprintln(writer, "ID\tName\tStatus\tDate")
					for _, req := range prs {
						fmt.Fprintf(
							writer,
							"%d\t%s\t[%s]\t%s\n",
							req.ID,
							req.Name,
							req.Status,
							req.CreatedAt.Format(time.RFC3339Nano),
						)
					}
					writer.Flush()
				} else if subCmd == "submitPatchRequest" {
					request, err := pr.SubmitPatchRequest(pubkey, repoID, sesh)
					if err != nil {
						wish.Fatalln(sesh, err)
						return
					}
					wish.Printf(sesh, "Patch Request submitted! Use the ID for interacting with this Patch Request.\nID\tName\n%d\t%s\n", request.ID, request.Name)
				} else if subCmd == "patchRequest" {
					if *prLs {
						_, err := pr.GetPatchRequestByID(prID)
						if err != nil {
							wish.Fatalln(sesh, err)
							return
						}

						patches, err := pr.GetPatchesByPrID(prID)
						if err != nil {
							wish.Fatalln(sesh, err)
							return
						}

						writer := NewTabWriter(sesh)
						fmt.Fprintln(writer, "ID\tTitle\tReview\tAuthor\tDate")
						for _, patch := range patches {
							fmt.Fprintf(
								writer,
								"%d\t%s\t%t\t%s\t%s\n",
								patch.ID,
								patch.Title,
								patch.Review,
								patch.AuthorName,
								patch.AuthorDate.Format(time.RFC3339Nano),
							)
						}
						writer.Flush()
						return
					}

					if *stats {
						request, err := pr.GetPatchRequestByID(prID)
						if err != nil {
							wish.Fatalln(sesh, err)
							return
						}

						writer := NewTabWriter(sesh)
						fmt.Fprintln(writer, "ID\tName\tStatus\tDate")
						fmt.Fprintf(
							writer,
							"%d\t%s\t[%s]\t%s\n%s\n\n",
							request.ID, request.Name, request.Status, request.CreatedAt.Format(time.RFC3339Nano),
							request.Text,
						)
						writer.Flush()

						patches, err := pr.GetPatchesByPrID(prID)
						if err != nil {
							wish.Fatalln(sesh, err)
							return
						}

						for _, patch := range patches {
							reviewTxt := ""
							if patch.Review {
								reviewTxt = "[review]"
							}
							wish.Printf(
								sesh,
								"%s %s %s\n%s <%s>\n%s\n\n---\n%s\n%s\n\n\n",
								patch.Title,
								reviewTxt,
								patch.CommitSha,
								patch.AuthorName,
								patch.AuthorEmail,
								patch.AuthorDate.Format(time.RFC3339Nano),
								patch.BodyAppendix,
								patch.Body,
							)
						}
						return
					}

					if *out {
						patches, err := pr.GetPatchesByPrID(prID)
						if err != nil {
							wish.Fatalln(sesh, err)
							return
						}

						if len(patches) == 1 {
							wish.Println(sesh, patches[0].RawText)
							return
						}

						for _, patch := range patches {
							wish.Printf(sesh, "%s\n\n\n", patch.RawText)
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
						reviewTxt := ""
						if *review {
							err = pr.UpdatePatchRequest(prID, "review")
							if err != nil {
								wish.Fatalln(sesh, err)
								return
							}
							reviewTxt = "[review]"
						}

						wish.Println(sesh, "Patch submitted!")
						writer := NewTabWriter(sesh)
						fmt.Fprintln(
							writer,
							"ID\tTitle",
						)
						fmt.Fprintf(
							writer,
							"%d\t%s %s\n",
							patch.ID,
							patch.Title,
							reviewTxt,
						)
						writer.Flush()
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
