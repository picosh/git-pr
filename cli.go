package git

import (
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/charmbracelet/soft-serve/pkg/utils"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/urfave/cli/v2"
)

func NewTabWriter(out io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(out, 0, 0, 1, ' ', tabwriter.TabIndent)
}

func getPrID(str string) (int64, error) {
	prID, err := strconv.ParseInt(str, 10, 64)
	return prID, err
}

func NewCli(sesh ssh.Session, be *Backend, pr GitPatchRequest) *cli.App {
	pubkey := be.Pubkey(sesh.PublicKey())
	app := &cli.App{
		Name:        "ssh",
		Description: "A companion SSH server to allow external collaboration",
		Writer:      sesh,
		ErrWriter:   sesh,
		Commands: []*cli.Command{
			{
				Name:  "git-receive-pack",
				Usage: "Receive what is pushed into the repository",
				Action: func(cCtx *cli.Context) error {
					repoName := cCtx.Args().First()
					err := gitServiceCommands(sesh, be, "git-receive-patch", repoName)
					return err
				},
			},
			{
				Name:  "git-upload-pack",
				Usage: "Send objects packed back to git-fetch-pack",
				Action: func(cCtx *cli.Context) error {
					repoName := cCtx.Args().First()
					err := gitServiceCommands(sesh, be, "git-upload-patch", repoName)
					return err
				},
			},
			{
				Name:  "ls",
				Usage: "list all git repos",
				Action: func(cCtx *cli.Context) error {
					repos, err := pr.GetRepos()
					if err != nil {
						return err
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
					return nil
				},
			},
			{
				Name:  "pr",
				Usage: "manage patch requests",
				Subcommands: []*cli.Command{
					{
						Name:  "ls",
						Usage: "list all patch requests",
						Action: func(cCtx *cli.Context) error {
							prs, err := pr.GetPatchRequests()
							if err != nil {
								return err
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
							return nil
						},
					},
					{
						Name:  "create",
						Usage: "submit a new patch request",
						Action: func(cCtx *cli.Context) error {
							repoID := cCtx.Args().First()
							request, err := pr.SubmitPatchRequest(pubkey, repoID, sesh)
							if err != nil {
								return err
							}
							wish.Printf(
								sesh,
								"Patch Request submitted! Use the ID for interacting with this Patch Request.\nID\tName\n%d\t%s\n",
								request.ID,
								request.Name,
							)
							return nil
						},
					},
					{
						Name:  "print",
						Usage: "print the patches for a patch request",
						Action: func(cCtx *cli.Context) error {
							prID, err := getPrID(cCtx.Args().First())
							if err != nil {
								return err
							}

							patches, err := pr.GetPatchesByPrID(prID)
							if err != nil {
								return err
							}

							if len(patches) == 1 {
								wish.Println(sesh, patches[0].RawText)
								return nil
							}

							for _, patch := range patches {
								wish.Printf(sesh, "%s\n\n\n", patch.RawText)
							}

							return nil
						},
					},
					{
						Name:  "stats",
						Usage: "print patch request with patch stats",
						Action: func(cCtx *cli.Context) error {
							prID, err := getPrID(cCtx.Args().First())
							if err != nil {
								return err
							}

							request, err := pr.GetPatchRequestByID(prID)
							if err != nil {
								return err
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
								return err
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

							return nil
						},
					},
					{
						Name:  "summary",
						Usage: "list patches in patch request",
						Action: func(cCtx *cli.Context) error {
							prID, err := getPrID(cCtx.Args().First())
							if err != nil {
								return err
							}
							request, err := pr.GetPatchRequestByID(prID)
							if err != nil {
								return err
							}

							writer := NewTabWriter(sesh)
							fmt.Fprintln(writer, "ID\tName\tStatus\tDate")
							fmt.Fprintf(
								writer,
								"%d\t%s\t[%s]\t%s\n%s\n",
								request.ID, request.Name, request.Status, request.CreatedAt.Format(time.RFC3339Nano),
								request.Text,
							)
							writer.Flush()

							patches, err := pr.GetPatchesByPrID(prID)
							if err != nil {
								return err
							}

							w := NewTabWriter(sesh)
							fmt.Fprintln(w, "Title\tStatus\tCommit\tAuthor\tDate")
							for _, patch := range patches {
								reviewTxt := ""
								if patch.Review {
									reviewTxt = "[review]"
								}
								fmt.Fprintf(
									w,
									"%s\t%s\t%s\t%s <%s>\t%s\n",
									patch.Title,
									reviewTxt,
									patch.CommitSha,
									patch.AuthorName,
									patch.AuthorEmail,
									patch.AuthorDate.Format(time.RFC3339Nano),
								)
							}
							w.Flush()

							return nil
						},
					},
					{
						Name:  "accept",
						Usage: "accept a patch request",
						Action: func(cCtx *cli.Context) error {
							prID, err := getPrID(cCtx.Args().First())
							if err != nil {
								return err
							}
							if !be.IsAdmin(sesh.PublicKey()) {
								return fmt.Errorf("must be admin to accpet PR")
							}
							err = pr.UpdatePatchRequest(prID, "accept")
							return err
						},
					},
					{
						Name:  "close",
						Usage: "close a patch request",
						Action: func(cCtx *cli.Context) error {
							prID, err := getPrID(cCtx.Args().First())
							if err != nil {
								return err
							}
							if !be.IsAdmin(sesh.PublicKey()) {
								return fmt.Errorf("must be admin to close PR")
							}
							err = pr.UpdatePatchRequest(prID, "close")
							return err
						},
					},
					{
						Name:  "reopen",
						Usage: "reopen a patch request",
						Action: func(cCtx *cli.Context) error {
							prID, err := getPrID(cCtx.Args().First())
							if err != nil {
								return err
							}
							if !be.IsAdmin(sesh.PublicKey()) {
								return fmt.Errorf("must be admin to close PR")
							}
							err = pr.UpdatePatchRequest(prID, "open")
							return err
						},
					},
					{
						Name:  "add",
						Usage: "append a patch to the patch request",
						Flags: []cli.Flag{
							&cli.BoolFlag{
								Name:  "review",
								Usage: "mark patch as a review",
							},
						},
						Action: func(cCtx *cli.Context) error {
							prID, err := getPrID(cCtx.Args().First())
							if err != nil {
								return err
							}
							isAdmin := be.IsAdmin(sesh.PublicKey())
							isReview := cCtx.Bool("review")
							var req PatchRequest
							err = be.DB.Get(&req, "SELECT * FROM patch_requests WHERE id=?", prID)
							if err != nil {
								return err
							}
							isPrOwner := req.Pubkey == be.Pubkey(sesh.PublicKey())
							if !isAdmin && !isPrOwner {
								return fmt.Errorf("unauthorized, you are not the owner of this Patch Request")
							}

							patch, err := pr.SubmitPatch(pubkey, prID, isReview, sesh)
							if err != nil {
								return err
							}
							reviewTxt := ""
							if isReview {
								err = pr.UpdatePatchRequest(prID, "review")
								if err != nil {
									return err
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
							return nil
						},
					},
					{
						Name:  "comment",
						Usage: "comment on a patch request",
						Action: func(cCtx *cli.Context) error {
							return nil
						},
					},
				},
			},
		},
	}

	return app
}