package git

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

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

func prSummary(be *Backend, pr GitPatchRequest, sesh ssh.Session, prID int64) error {
	request, err := pr.GetPatchRequestByID(prID)
	if err != nil {
		return err
	}

	repo, err := pr.GetRepoByID(request.RepoID)
	if err != nil {
		return err
	}

	repoUser, err := pr.GetUserByID(repo.UserID)
	if err != nil {
		return err
	}

	wish.Printf(sesh, "Info\n====\n")
	wish.Printf(sesh, "URL: https://%s/prs/%d\n", be.Cfg.Url, prID)
	wish.Printf(sesh, "Repo: %s\n\n", be.CreateRepoNs(repoUser.Name, repo.Name))

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
					repoNs := cCtx.String("repo")
					var eventLogs []*EventLog
					if isPubkey {
						eventLogs, err = pr.GetEventLogsByUserID(user.ID)
					} else if prID != 0 {
						eventLogs, err = pr.GetEventLogsByPrID(prID)
					} else if repoNs != "" {
						repoUsername, repoName := be.SplitRepoNs(repoNs)
						var repoUser *User
						repoUser, err = pr.GetUserByName(repoUsername)
						if err != nil {
							return nil
						}
						eventLogs, err = pr.GetEventLogsByRepoName(repoUser, repoName)
					} else {
						eventLogs, err = pr.GetEventLogs()
					}
					if err != nil {
						return err
					}

					writer := NewTabWriter(sesh)
					fmt.Fprintln(writer, "RepoID\tPrID\tPatchsetID\tEvent\tCreated\tData")
					for _, eventLog := range eventLogs {
						repo, err := pr.GetRepoByID(eventLog.RepoID.Int64)
						if err != nil {
							be.Logger.Error("repo not found", "repo", repo, "err", err)
							continue
						}
						repoUser, err := pr.GetUserByID(repo.UserID)
						if err != nil {
							be.Logger.Error("repo user not found", "repo", repo, "err", err)
							continue
						}
						fmt.Fprintf(
							writer,
							"%s\t%d\t%s\t%s\t%s\t%s\n",
							be.CreateRepoNs(repoUser.Name, repo.Name),
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
				Name:  "repo",
				Usage: "Manage repos",
				Subcommands: []*cli.Command{
					{
						Name:      "create",
						Usage:     "Create a new repo",
						Args:      true,
						ArgsUsage: "[repoName]",
						Action: func(cCtx *cli.Context) error {
							user, err := pr.UpsertUser(pubkey, userName)
							if err != nil {
								return err
							}

							args := cCtx.Args()
							if !args.Present() {
								return fmt.Errorf("need repo name argument")
							}
							repoName := args.First()
							repo, _ := pr.GetRepoByName(user, repoName)
							err = be.CanCreateRepo(repo, user)
							if err != nil {
								return err
							}

							if repo == nil {
								repo, err = pr.CreateRepo(user, repoName)
								if err != nil {
									return err
								}
							}

							wish.Printf(sesh, "repo created: %s/%s", user.Name, repo.Name)
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
						ArgsUsage: "[repoName]",
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
							rawRepoNs := args.First()
							userName, repoName := be.SplitRepoNs(rawRepoNs)
							var prs []*PatchRequest
							var err error
							if repoName == "" {
								prs, err = pr.GetPatchRequests()
								if err != nil {
									return err
								}
							} else {
								user, err := pr.GetUserByName(userName)
								if err != nil {
									return err
								}
								repo, err := pr.GetRepoByName(user, repoName)
								if err != nil {
									return err
								}
								prs, err = pr.GetPatchRequestsByRepoID(repo.ID)
								if err != nil {
									return err
								}
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

								repo, err := pr.GetRepoByID(req.RepoID)
								if err != nil {
									be.Logger.Error("could not get repo for pr", "err", err)
									continue
								}

								repoUser, err := pr.GetUserByID(repo.UserID)
								if err != nil {
									be.Logger.Error("could not get repo user for pr", "err", err)
									continue
								}

								fmt.Fprintf(
									writer,
									"%d\t%s\t%s\t[%s]\t%d\t%s\t%s\n",
									req.ID,
									be.CreateRepoNs(repoUser.Name, repo.Name),
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
						ArgsUsage: "[repoName]",
						Action: func(cCtx *cli.Context) error {
							user, err := pr.UpsertUser(pubkey, userName)
							if err != nil {
								return err
							}

							args := cCtx.Args()
							rawRepoNs := "bin"
							if args.Present() {
								rawRepoNs = args.First()
							}
							repoUsername, repoName := be.SplitRepoNs(rawRepoNs)
							var repo *Repo
							if repoUsername == "" {
								if be.Cfg.CreateRepo == "admin" {
									// single tenant default user to admin
									repo, _ = pr.GetRepoByName(nil, repoName)
								} else {
									// multi tenant default user to contributor
									repo, _ = pr.GetRepoByName(user, repoName)
								}
							} else {
								repoUser, err := pr.GetUserByName(repoUsername)
								if err != nil {
									return err
								}
								repo, _ = pr.GetRepoByName(repoUser, repoName)
							}

							err = be.CanCreateRepo(repo, user)
							if err != nil {
								return err
							}

							if repo == nil {
								repo, err = pr.CreateRepo(user, repoName)
								if err != nil {
									return err
								}
							}

							prq, err := pr.SubmitPatchRequest(repo.ID, user.ID, sesh)
							if err != nil {
								return err
							}
							wish.Println(
								sesh,
								"PR submitted! Use the ID for interacting with this PR.",
							)

							return prSummary(be, pr, sesh, prq.ID)
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

							rangeDiff, err := pr.DiffPatchsets(prev, latest)
							if err != nil {
								be.Logger.Error("could not diff patchset", "err", err)
								return err
							}

							wish.Println(sesh, RangeDiffToStr(rangeDiff))
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
							return prSummary(be, pr, sesh, prID)
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

							prq, err := pr.GetPatchRequestByID(prID)
							if err != nil {
								return err
							}

							user, err := pr.UpsertUser(pubkey, userName)
							if err != nil {
								return err
							}

							repo, err := pr.GetRepoByID(prq.RepoID)
							if err != nil {
								return err
							}

							acl := be.GetPatchRequestAcl(repo, prq, user)
							if !acl.CanReview {
								return fmt.Errorf("you are not authorized to accept a PR")
							}

							if prq.Status == "accepted" {
								return fmt.Errorf("PR has already been accepted")
							}

							err = pr.UpdatePatchRequestStatus(prID, user.ID, "accepted")
							if err != nil {
								return err
							}
							wish.Printf(sesh, "Accepted PR %s (#%d)\n", prq.Name, prq.ID)
							return prSummary(be, pr, sesh, prID)
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

							prq, err := pr.GetPatchRequestByID(prID)
							if err != nil {
								return err
							}

							patchUser, err := pr.GetUserByID(prq.UserID)
							if err != nil {
								return err
							}

							repo, err := pr.GetRepoByID(prq.RepoID)
							if err != nil {
								return err
							}

							acl := be.GetPatchRequestAcl(repo, prq, patchUser)
							if !acl.CanModify {
								return fmt.Errorf("you are not authorized to change PR status")
							}

							if prq.Status == "closed" {
								return fmt.Errorf("PR has already been closed")
							}

							user, err := pr.UpsertUser(pubkey, userName)
							if err != nil {
								return err
							}

							err = pr.UpdatePatchRequestStatus(prID, user.ID, "closed")
							if err != nil {
								return err
							}
							wish.Printf(sesh, "Closed PR %s (#%d)\n", prq.Name, prq.ID)
							return prSummary(be, pr, sesh, prID)
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

							prq, err := pr.GetPatchRequestByID(prID)
							if err != nil {
								return err
							}

							patchUser, err := pr.GetUserByID(prq.UserID)
							if err != nil {
								return err
							}

							repo, err := pr.GetRepoByID(prq.RepoID)
							if err != nil {
								return err
							}

							acl := be.GetPatchRequestAcl(repo, prq, patchUser)
							if !acl.CanModify {
								return fmt.Errorf("you are not authorized to change PR status")
							}

							if prq.Status == "open" {
								return fmt.Errorf("PR is already open")
							}

							user, err := pr.UpsertUser(pubkey, userName)
							if err != nil {
								return err
							}

							err = pr.UpdatePatchRequestStatus(prID, user.ID, "open")
							if err == nil {
								wish.Printf(sesh, "Reopened PR %s (#%d)\n", prq.Name, prq.ID)
							}
							return prSummary(be, pr, sesh, prID)
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

							repo, err := pr.GetRepoByID(prq.RepoID)
							if err != nil {
								return err
							}

							acl := be.GetPatchRequestAcl(repo, prq, user)
							if !acl.CanModify {
								return fmt.Errorf("you are not authorized to change PR")
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
								Usage: "submit patchset and mark PR as reviewed",
							},
							&cli.BoolFlag{
								Name:  "accept",
								Usage: "submit patchset and mark PR as accepted",
							},
							&cli.BoolFlag{
								Name:  "close",
								Usage: "submit patchset and mark PR as closed",
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

							isReview := cCtx.Bool("review")
							isAccept := cCtx.Bool("accept")
							isClose := cCtx.Bool("close")

							repo, err := pr.GetRepoByID(prq.RepoID)
							if err != nil {
								return err
							}

							acl := be.GetPatchRequestAcl(repo, prq, user)
							if !acl.CanAddPatchset {
								return fmt.Errorf("you are not authorized to add patchsets to pr")
							}

							if isReview && !acl.CanReview {
								return fmt.Errorf("you are not authorized to submit a review to pr")
							}

							op := OpNormal
							nextStatus := "open"
							if isReview {
								wish.Println(sesh, "Marking PR as reviewed")
								nextStatus = "reviewed"
								op = OpReview
							} else if isAccept {
								wish.Println(sesh, "Marking PR as accepted")
								nextStatus = "accepted"
								op = OpAccept
							} else if isClose {
								wish.Println(sesh, "Marking PR as closed")
								nextStatus = "closed"
								op = OpClose
							}

							patches, err := pr.SubmitPatchset(prID, user.ID, op, sesh)
							if err != nil {
								return err
							}

							if len(patches) == 0 {
								wish.Println(sesh, "Patches submitted! However none were saved, probably because they already exist in the system")
								return nil
							}

							if prq.Status != nextStatus {
								err = pr.UpdatePatchRequestStatus(prID, user.ID, nextStatus)
								if err != nil {
									return err
								}
							}

							wish.Println(sesh, "Patches submitted!")
							return prSummary(be, pr, sesh, prID)
						},
					},
				},
			},
		},
	}

	return app
}
