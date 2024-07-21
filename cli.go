package git

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/charmbracelet/soft-serve/pkg/utils"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/urfave/cli/v2"
)

func NewTabWriter(out io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(out, 0, 0, 1, ' ', tabwriter.TabIndent)
}

func strToInt(str string) (int64, error) {
	prID, err := strconv.ParseInt(str, 10, 64)
	return prID, err
}

func getPatchsetFromOpt(patchsets []*Patchset, optPatchsetID string) (*Patchset, error) {
	if optPatchsetID == "" {
		return patchsets[len(patchsets)-1], nil
	}

	id, err := getPatchsetID(optPatchsetID)
	if err != nil {
		return nil, err
	}

	for _, ps := range patchsets {
		if ps.ID == id {
			return ps, nil
		}
	}

	return nil, fmt.Errorf("cannot find patchset: %s", optPatchsetID)
}

func printPatches(sesh ssh.Session, patches []*Patch) {
	if len(patches) == 1 {
		wish.Println(sesh, patches[0].RawText)
		return
	}

	opatches := patches
	for idx, patch := range opatches {
		wish.Println(sesh, patch.RawText)
		if idx < len(patches)-1 {
			wish.Printf(sesh, "\n\n\n")
		}
	}
}

func NewCli(sesh ssh.Session, be *Backend, pr GitPatchRequest) *cli.App {
	desc := `Patch requests (PR) are the simplest way to submit, review, and accept changes to your git repository.
Here's how it works:
	- External contributor clones repo (git-clone)
	- External contributor makes a code change (git-add & git-commit)
	- External contributor generates patches (git-format-patch)
	- External contributor submits a PR to SSH server
	- Owner receives RSS notification that there's a new PR
	- Owner applies patches locally (git-am) from SSH server
	- Owner makes suggestions in code! (git-add & git-commit)
	- Owner submits review by piping patch to SSH server (git-format-patch)
	- External contributor receives RSS notification of the PR review
	- External contributor re-applies patches (git-am)
	- External contributor reviews and removes comments in code!
	- External contributor submits another patch (git-format-patch)
	- Owner applies patches locally (git-am)
	- Owner marks PR as accepted and pushes code to main (git-push)`

	pubkey := be.Pubkey(sesh.PublicKey())
	userName := sesh.User()
	app := &cli.App{
		Name:        "ssh",
		Description: desc,
		Usage:       "Collaborate with contributors for your git project",
		Writer:      sesh,
		ErrWriter:   sesh,
		ExitErrHandler: func(cCtx *cli.Context, err error) {
			if err != nil {
				wish.Fatalln(sesh, fmt.Errorf("err: %w", err))
			}
		},
		OnUsageError: func(cCtx *cli.Context, err error, isSubcommand bool) error {
			if err != nil {
				wish.Fatalln(sesh, fmt.Errorf("err: %w", err))
			}
			return nil
		},
		Commands: []*cli.Command{
			/* {
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
			}, */
			{
				Name:  "ls",
				Usage: "List all git repos",
				Action: func(cCtx *cli.Context) error {
					repos, err := pr.GetRepos()
					if err != nil {
						return err
					}
					writer := NewTabWriter(sesh)
					fmt.Fprintln(writer, "ID\tDefBranch\tClone\tDesc")
					for _, repo := range repos {
						fmt.Fprintf(
							writer,
							"%s\t%s\t%s\t%s\n",
							utils.SanitizeRepo(repo.ID),
							repo.DefaultBranch,
							repo.CloneAddr,
							repo.Desc,
						)
					}
					writer.Flush()
					return nil
				},
			},
			{
				Name:  "logs",
				Usage: "List event logs with filters",
				Args:  true,
				Flags: []cli.Flag{
					&cli.Int64Flag{
						Name:  "pr",
						Usage: "show all events related to the provided patch request",
					},
					&cli.BoolFlag{
						Name:  "pubkey",
						Usage: "show all events related to your pubkey",
					},
					&cli.StringFlag{
						Name:  "repo",
						Usage: "show all events related to a repo",
					},
				},
				Action: func(cCtx *cli.Context) error {
					pubkey := be.Pubkey(sesh.PublicKey())
					user, err := pr.GetUserByPubkey(pubkey)
					if err != nil {
						return err
					}
					isPubkey := cCtx.Bool("pubkey")
					prID := cCtx.Int64("pr")
					repoID := cCtx.String("repo")
					var eventLogs []*EventLog
					if isPubkey {
						eventLogs, err = pr.GetEventLogsByUserID(user.ID)
					} else if prID != 0 {
						eventLogs, err = pr.GetEventLogsByPrID(prID)
					} else if repoID != "" {
						eventLogs, err = pr.GetEventLogsByRepoID(repoID)
					} else {
						eventLogs, err = pr.GetEventLogs()
					}
					if err != nil {
						return err
					}
					writer := NewTabWriter(sesh)
					fmt.Fprintln(writer, "RepoID\tPrID\tPatchsetID\tEvent\tCreated\tData")
					for _, eventLog := range eventLogs {
						fmt.Fprintf(
							writer,
							"%s\t%d\t%s\t%s\t%s\t%s\n",
							eventLog.RepoID,
							eventLog.PatchRequestID.Int64,
							getFormattedPatchsetID(eventLog.PatchsetID.Int64),
							eventLog.Event,
							eventLog.CreatedAt.Format(be.Cfg.TimeFormat),
							eventLog.Data,
						)
					}
					writer.Flush()
					return nil
				},
			},
			{
				Name:  "ps",
				Usage: "Mange patchsets",
				Subcommands: []*cli.Command{
					{
						Name:      "rm",
						Usage:     "Remove a patchset with its patches",
						Args:      true,
						ArgsUsage: "[patchsetID]",
						Action: func(cCtx *cli.Context) error {
							args := cCtx.Args()
							if !args.Present() {
								return fmt.Errorf("must provide a patchset ID")
							}

							patchsetID, err := getPatchsetID(args.First())
							if err != nil {
								return err
							}

							patchset, err := pr.GetPatchsetByID(patchsetID)
							if err != nil {
								return err
							}

							user, err := pr.GetUserByID(patchset.UserID)
							if err != nil {
								return err
							}

							pk := sesh.PublicKey()
							isAdmin := be.IsAdmin(pk)
							isContrib := pubkey == user.Pubkey
							if !isAdmin && !isContrib {
								return fmt.Errorf("you are not authorized to delete a patchset")
							}

							err = pr.DeletePatchsetByID(user.ID, patchset.PatchRequestID, patchsetID)
							if err != nil {
								return err
							}
							wish.Printf(sesh, "successfully removed patchset: %d\n", patchsetID)
							return nil
						},
					},
				},
			},
			{
				Name:  "pr",
				Usage: "Manage Patch Requests (PR)",
				Subcommands: []*cli.Command{
					{
						Name:      "ls",
						Usage:     "List all PRs",
						Args:      true,
						ArgsUsage: "[repoID]",
						Flags: []cli.Flag{
							&cli.BoolFlag{
								Name:  "open",
								Usage: "only show open PRs",
							},
							&cli.BoolFlag{
								Name:  "closed",
								Usage: "only show closed PRs",
							},
							&cli.BoolFlag{
								Name:  "accepted",
								Usage: "only show accepted PRs",
							},
							&cli.BoolFlag{
								Name:  "reviewed",
								Usage: "only show reviewed PRs",
							},
							&cli.BoolFlag{
								Name:  "mine",
								Usage: "only show your own PRs",
							},
						},
						Action: func(cCtx *cli.Context) error {
							args := cCtx.Args()
							repoID := args.First()
							var err error
							var prs []*PatchRequest
							if repoID == "" {
								prs, err = pr.GetPatchRequests()
							} else {
								prs, err = pr.GetPatchRequestsByRepoID(repoID)
							}
							if err != nil {
								return err
							}

							onlyOpen := cCtx.Bool("open")
							onlyAccepted := cCtx.Bool("accepted")
							onlyClosed := cCtx.Bool("closed")
							onlyReviewed := cCtx.Bool("reviewed")
							onlyMine := cCtx.Bool("mine")

							writer := NewTabWriter(sesh)
							fmt.Fprintln(writer, "ID\tRepoID\tName\tStatus\tPatchsets\tUser\tDate")
							for _, req := range prs {
								if onlyAccepted && req.Status != "accepted" {
									continue
								}

								if onlyClosed && req.Status != "closed" {
									continue
								}

								if onlyOpen && req.Status != "open" {
									continue
								}

								if onlyReviewed && req.Status != "reviewed" {
									continue
								}

								user, err := pr.GetUserByID(req.UserID)
								if err != nil {
									be.Logger.Error("could not get user for pr", "err", err)
									continue
								}

								if onlyMine && user.Name != userName {
									continue
								}

								patchsets, err := pr.GetPatchsetsByPrID(req.ID)
								if err != nil {
									be.Logger.Error("could not get patchsets for pr", "err", err)
									continue
								}

								fmt.Fprintf(
									writer,
									"%d\t%s\t%s\t[%s]\t%d\t%s\t%s\n",
									req.ID,
									req.RepoID,
									req.Name,
									req.Status,
									len(patchsets),
									user.Name,
									req.CreatedAt.Format(be.Cfg.TimeFormat),
								)
							}
							writer.Flush()
							return nil
						},
					},
					{
						Name:      "create",
						Usage:     "Submit a new PR",
						Args:      true,
						ArgsUsage: "[repoID]",
						Action: func(cCtx *cli.Context) error {
							user, err := pr.UpsertUser(pubkey, userName)
							if err != nil {
								return err
							}

							args := cCtx.Args()
							if !args.Present() {
								return fmt.Errorf("must provide a repo ID")
							}

							repoID := args.First()
							prq, err := pr.SubmitPatchRequest(repoID, user.ID, sesh)
							if err != nil {
								return err
							}
							wish.Println(
								sesh,
								"PR submitted! Use the ID for interacting with this PR.",
							)

							writer := NewTabWriter(sesh)
							fmt.Fprintln(writer, "ID\tName\tURL")
							fmt.Fprintf(
								writer,
								"%d\t%s\t%s\n",
								prq.ID,
								prq.Name,
								fmt.Sprintf("https://%s/prs/%d", be.Cfg.Url, prq.ID),
							)
							writer.Flush()

							return nil
						},
					},
					{
						Name:      "diff",
						Usage:     "Print a diff between the last two patchsets in a PR",
						Args:      true,
						ArgsUsage: "[prID]",
						Action: func(cCtx *cli.Context) error {
							args := cCtx.Args()
							if !args.Present() {
								return fmt.Errorf("must provide a patch request ID")
							}

							prID, err := strToInt(args.First())
							if err != nil {
								return err
							}

							patchsets, err := pr.GetPatchsetsByPrID(prID)
							if err != nil {
								be.Logger.Error("cannot get latest patchset", "err", err)
								return err
							}

							if len(patchsets) == 0 {
								return fmt.Errorf("no patchsets found for pr: %d", prID)
							}

							latest := patchsets[len(patchsets)-1]
							var prev *Patchset
							if len(patchsets) > 1 {
								prev = patchsets[len(patchsets)-2]
							}

							patches, err := pr.DiffPatchsets(prev, latest)
							if err != nil {
								be.Logger.Error("could not diff patchset", "err", err)
								return err
							}

							printPatches(sesh, patches)
							return nil
						},
					},
					{
						Name:      "print",
						Usage:     "Print the patches for a PR",
						Args:      true,
						ArgsUsage: "[prID]",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:    "patchset",
								Usage:   "Provide patchset ID to print a specific patchset (`patchset-xxx`, default is latest)",
								Aliases: []string{"ps"},
							},
						},
						Action: func(cCtx *cli.Context) error {
							args := cCtx.Args()
							if !args.Present() {
								return fmt.Errorf("must provide a patch request ID")
							}

							prID, err := strToInt(args.First())
							if err != nil {
								return err
							}

							patchsets, err := pr.GetPatchsetsByPrID(prID)
							if err != nil {
								return err
							}

							patchset, err := getPatchsetFromOpt(patchsets, cCtx.String("patchset"))
							if err != nil {
								return err
							}

							patches, err := pr.GetPatchesByPatchsetID(patchset.ID)
							if err != nil {
								return err
							}

							printPatches(sesh, patches)
							return nil
						},
					},
					{
						Name:      "summary",
						Usage:     "Display metadata related to a PR",
						Args:      true,
						ArgsUsage: "[prID]",
						Action: func(cCtx *cli.Context) error {
							args := cCtx.Args()
							if !args.Present() {
								return fmt.Errorf("must provide a patch request ID")
							}

							prID, err := strToInt(args.First())
							if err != nil {
								return err
							}
							request, err := pr.GetPatchRequestByID(prID)
							if err != nil {
								return err
							}

							wish.Printf(sesh, "Info\n====\n")

							writer := NewTabWriter(sesh)
							fmt.Fprintln(writer, "ID\tName\tStatus\tDate")
							fmt.Fprintf(
								writer,
								"%d\t%s\t[%s]\t%s\n",
								request.ID, request.Name, request.Status, request.CreatedAt.Format(be.Cfg.TimeFormat),
							)
							writer.Flush()

							patchsets, err := pr.GetPatchsetsByPrID(prID)
							if err != nil {
								return err
							}

							wish.Printf(sesh, "\nPatchsets\n====\n")

							writerSet := NewTabWriter(sesh)
							fmt.Fprintln(writerSet, "ID\tType\tUser\tDate")
							for _, patchset := range patchsets {
								user, err := pr.GetUserByID(patchset.UserID)
								if err != nil {
									be.Logger.Error("cannot find user for patchset", "err", err)
									continue
								}
								isReview := ""
								if patchset.Review {
									isReview = "[review]"
								}

								fmt.Fprintf(
									writerSet,
									"%s\t%s\t%s\t%s\n",
									getFormattedPatchsetID(patchset.ID),
									isReview,
									user.Name,
									patchset.CreatedAt.Format(be.Cfg.TimeFormat),
								)
							}
							writerSet.Flush()

							latest, err := getPatchsetFromOpt(patchsets, "")
							if err != nil {
								return err
							}

							patches, err := pr.GetPatchesByPatchsetID(latest.ID)
							if err != nil {
								return err
							}

							wish.Printf(sesh, "\nPatches from latest patchset\n====\n")

							opatches := patches
							w := NewTabWriter(sesh)
							fmt.Fprintln(w, "Idx\tTitle\tCommit\tAuthor\tDate")
							for idx, patch := range opatches {
								timestamp := patch.AuthorDate.Format(be.Cfg.TimeFormat)
								fmt.Fprintf(
									w,
									"%d\t%s\t%s\t%s <%s>\t%s\n",
									idx,
									patch.Title,
									truncateSha(patch.CommitSha),
									patch.AuthorName,
									patch.AuthorEmail,
									timestamp,
								)
							}
							w.Flush()

							return nil
						},
					},
					{
						Name:      "accept",
						Usage:     "Accept a PR",
						Args:      true,
						ArgsUsage: "[prID]",
						Action: func(cCtx *cli.Context) error {
							args := cCtx.Args()
							if !args.Present() {
								return fmt.Errorf("must provide a patch request ID")
							}

							prID, err := strToInt(args.First())
							if err != nil {
								return err
							}

							isAdmin := be.IsAdmin(sesh.PublicKey())
							if !isAdmin {
								return fmt.Errorf("you are not authorized to accept a PR")
							}

							patchReq, err := pr.GetPatchRequestByID(prID)
							if err != nil {
								return err
							}

							if patchReq.Status == "accepted" {
								return fmt.Errorf("PR has already been accepted")
							}

							user, err := pr.UpsertUser(pubkey, userName)
							if err != nil {
								return err
							}

							err = pr.UpdatePatchRequestStatus(prID, user.ID, "accepted")
							if err == nil {
								wish.Printf(sesh, "Accepted PR %s (#%d)\n", patchReq.Name, patchReq.ID)
							}
							return err
						},
					},
					{
						Name:      "close",
						Usage:     "Close a PR",
						Args:      true,
						ArgsUsage: "[prID]",
						Action: func(cCtx *cli.Context) error {
							args := cCtx.Args()
							if !args.Present() {
								return fmt.Errorf("must provide a patch request ID")
							}

							prID, err := strToInt(args.First())
							if err != nil {
								return err
							}

							patchReq, err := pr.GetPatchRequestByID(prID)
							if err != nil {
								return err
							}

							user, err := pr.GetUserByID(patchReq.UserID)
							if err != nil {
								return err
							}

							pk := sesh.PublicKey()
							isContrib := pubkey == user.Pubkey
							isAdmin := be.IsAdmin(pk)
							if !isAdmin && !isContrib {
								return fmt.Errorf("you are not authorized to change PR status")
							}

							if patchReq.Status == "closed" {
								return fmt.Errorf("PR has already been closed")
							}

							err = pr.UpdatePatchRequestStatus(prID, user.ID, "closed")
							if err == nil {
								wish.Printf(sesh, "Closed PR %s (#%d)\n", patchReq.Name, patchReq.ID)
							}
							return err
						},
					},
					{
						Name:      "reopen",
						Usage:     "Reopen a PR",
						Args:      true,
						ArgsUsage: "[prID]",
						Action: func(cCtx *cli.Context) error {
							args := cCtx.Args()
							if !args.Present() {
								return fmt.Errorf("must provide a patch request ID")
							}

							prID, err := strToInt(args.First())
							if err != nil {
								return err
							}

							patchReq, err := pr.GetPatchRequestByID(prID)
							if err != nil {
								return err
							}

							user, err := pr.GetUserByID(patchReq.UserID)
							if err != nil {
								return err
							}

							pk := sesh.PublicKey()
							isContrib := pubkey == user.Pubkey
							isAdmin := be.IsAdmin(pk)
							if !isAdmin && !isContrib {
								return fmt.Errorf("you are not authorized to change PR status")
							}

							if patchReq.Status == "open" {
								return fmt.Errorf("PR is already open")
							}

							err = pr.UpdatePatchRequestStatus(prID, user.ID, "open")
							if err == nil {
								wish.Printf(sesh, "Reopened PR %s (#%d)\n", patchReq.Name, patchReq.ID)
							}
							return err
						},
					},
					{
						Name:      "edit",
						Usage:     "Edit PR title",
						Args:      true,
						ArgsUsage: "[prID] [title]",
						Action: func(cCtx *cli.Context) error {
							args := cCtx.Args()
							if !args.Present() {
								return fmt.Errorf("must provide a patch request ID")
							}

							prID, err := strToInt(args.First())
							if err != nil {
								return err
							}
							prq, err := pr.GetPatchRequestByID(prID)
							if err != nil {
								return err
							}

							user, err := pr.UpsertUser(pubkey, userName)
							if err != nil {
								return err
							}

							isAdmin := be.IsAdmin(sesh.PublicKey())
							isPrOwner := be.IsPrOwner(prq.UserID, user.ID)
							if !isAdmin && !isPrOwner {
								return fmt.Errorf("unauthorized, you are not the owner of this PR")
							}

							tail := cCtx.Args().Tail()
							title := strings.Join(tail, " ")
							if title == "" {
								return fmt.Errorf("must provide title")
							}

							err = pr.UpdatePatchRequestName(
								prID,
								user.ID,
								title,
							)
							if err == nil {
								wish.Printf(sesh, "New title: %s (%d)\n", title, prq.ID)
							}

							return err
						},
					},
					{
						Name:      "add",
						Usage:     "Add a new patchset to a PR",
						Args:      true,
						ArgsUsage: "[prID]",
						Flags: []cli.Flag{
							&cli.BoolFlag{
								Name:  "review",
								Usage: "mark patch as a review",
							},
						},
						Action: func(cCtx *cli.Context) error {
							args := cCtx.Args()
							if !args.Present() {
								return fmt.Errorf("must provide a patch request ID")
							}

							prID, err := strToInt(args.First())
							if err != nil {
								return err
							}
							prq, err := pr.GetPatchRequestByID(prID)
							if err != nil {
								return err
							}

							user, err := pr.UpsertUser(pubkey, userName)
							if err != nil {
								return err
							}

							isAdmin := be.IsAdmin(sesh.PublicKey())
							isReview := cCtx.Bool("review")
							isPrOwner := be.IsPrOwner(prq.UserID, user.ID)
							if !isAdmin && !isPrOwner {
								return fmt.Errorf("unauthorized, you are not the owner of this PR")
							}

							op := OpNormal
							if isReview {
								wish.Println(sesh, "Marking new patchset as a review")
								op = OpReview
							}

							patches, err := pr.SubmitPatchset(prID, user.ID, op, sesh)
							if err != nil {
								return err
							}

							if len(patches) == 0 {
								wish.Println(sesh, "Patches submitted! However none were saved, probably because they already exist in the system")
								return nil
							}

							reviewTxt := ""
							if isReview {
								err = pr.UpdatePatchRequestStatus(prID, user.ID, "reviewed")
								if err != nil {
									return err
								}
								reviewTxt = "[review]"
							}

							wish.Println(sesh, "Patches submitted!")
							writer := NewTabWriter(sesh)
							fmt.Fprintln(
								writer,
								"ID\tTitle",
							)
							for _, patch := range patches {
								fmt.Fprintf(
									writer,
									"%d\t%s %s\n",
									patch.ID,
									patch.Title,
									reviewTxt,
								)
							}
							writer.Flush()

							wish.Println(
								sesh,
								fmt.Sprintf("https://%s/prs/%d", be.Cfg.Url, prq.ID),
							)
							return nil
						},
					},
				},
			},
		},
	}

	return app
}
