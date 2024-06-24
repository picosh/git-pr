package git

import (
	"fmt"
	"io"
	"strconv"
	"strings"
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

type Ranger struct {
	Left  int
	Right int
}

func parseRange(rnge string, sliceLen int) (*Ranger, error) {
	items := strings.Split(rnge, ":")
	left := 0
	var err error
	if items[0] != "" {
		left, err = strconv.Atoi(items[0])
		if err != nil {
			return nil, fmt.Errorf("first value before `:` must provide number")
		}
	}

	if left < 0 {
		return nil, fmt.Errorf("first value must be >= 0")
	}

	if left >= sliceLen {
		return nil, fmt.Errorf("first value must be less than number of patches")
	}

	if len(items) == 1 {
		return &Ranger{
			Left:  left,
			Right: left,
		}, nil
	}

	if items[1] == "" {
		return &Ranger{Left: left, Right: sliceLen - 1}, nil
	}

	right, err := strconv.Atoi(items[1])
	if err != nil {
		return nil, fmt.Errorf("second value after `:` must provide number")
	}

	if left > right {
		return nil, fmt.Errorf("second value must be greater than first value")
	}

	if right >= sliceLen {
		return nil, fmt.Errorf("second value must be less than number of patches")
	}

	return &Ranger{
		Left:  left,
		Right: right,
	}, nil
}

func filterPatches(ranger *Ranger, patches []*Patch) []*Patch {
	if ranger.Left == ranger.Right {
		return []*Patch{patches[ranger.Left]}
	}
	return patches[ranger.Left:ranger.Right]
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
	app := &cli.App{
		Name:        "ssh",
		Description: desc,
		Usage:       "Collaborate with contributors for your git project",
		Writer:      sesh,
		ErrWriter:   sesh,
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
					fmt.Fprintln(writer, "ID")
					for _, repo := range repos {
						fmt.Fprintf(
							writer,
							"%s\n",
							utils.SanitizeRepo(repo.ID),
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
					isPubkey := cCtx.Bool("pubkey")
					prID := cCtx.Int64("pr")
					repoID := cCtx.String("repo")
					var eventLogs []*EventLog
					var err error
					if isPubkey {
						eventLogs, err = pr.GetEventLogsByPubkey(pubkey)
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
					fmt.Fprintln(writer, "RepoID\tPrID\tEvent\tCreated\tData")
					for _, eventLog := range eventLogs {
						fmt.Fprintf(
							writer,
							"%s\t%d\t%s\t%s\t%s\n",
							eventLog.RepoID,
							eventLog.PatchRequestID,
							eventLog.Event,
							eventLog.CreatedAt.Format(time.RFC3339Nano),
							eventLog.Data,
						)
					}
					writer.Flush()
					return nil
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
								Name:  "closed",
								Usage: "only show closed PRs",
							},
							&cli.BoolFlag{
								Name:  "accepted",
								Usage: "only show accepted PRs",
							},
						},
						Action: func(cCtx *cli.Context) error {
							repoID := cCtx.Args().First()
							var prs []*PatchRequest
							var err error
							if repoID == "" {
								prs, err = pr.GetPatchRequests()
							} else {
								prs, err = pr.GetPatchRequestsByRepoID(repoID)
							}
							if err != nil {
								return err
							}

							onlyAccepted := cCtx.Bool("accepted")
							onlyClosed := cCtx.Bool("closed")

							writer := NewTabWriter(sesh)
							fmt.Fprintln(writer, "ID\tRepoID\tName\tStatus\tDate")
							for _, req := range prs {
								if onlyAccepted && req.Status != "accepted" {
									continue
								}

								if onlyClosed && req.Status != "closed" {
									continue
								}

								if !onlyAccepted && !onlyClosed && req.Status != "open" {
									continue
								}

								fmt.Fprintf(
									writer,
									"%d\t%s\t%s\t[%s]\t%s\n",
									req.ID,
									req.RepoID,
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
						Name:      "create",
						Usage:     "Submit a new PR",
						Args:      true,
						ArgsUsage: "[repoID]",
						Action: func(cCtx *cli.Context) error {
							repoID := cCtx.Args().First()
							request, err := pr.SubmitPatchRequest(repoID, pubkey, sesh)
							if err != nil {
								return err
							}
							wish.Printf(
								sesh,
								"PR submitted! Use the ID for interacting with this PR.\nID\tName\n%d\t%s\n",
								request.ID,
								request.Name,
							)
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
								Name:    "filter",
								Usage:   "Only print patches in sequence range (x:y) (x:) (:y)",
								Aliases: []string{"f"},
							},
						},
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

							rnge := cCtx.String("filter")
							opatches := patches
							if rnge != "" {
								ranger, err := parseRange(rnge, len(patches))
								if err != nil {
									return err
								}
								opatches = filterPatches(ranger, patches)
							}

							for idx, patch := range opatches {
								wish.Println(sesh, patch.RawText)
								if idx < len(patches)-1 {
									wish.Printf(sesh, "\n\n\n")
								}
							}

							return nil
						},
					},
					{
						Name:      "stats",
						Usage:     "Print PR with diff stats",
						Args:      true,
						ArgsUsage: "[prID]",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:    "filter",
								Usage:   "Only print patches in sequence range (x:y) (x:) (:y)",
								Aliases: []string{"f"},
							},
						},
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

							rnge := cCtx.String("filter")
							opatches := patches
							if rnge != "" {
								ranger, err := parseRange(rnge, len(patches))
								if err != nil {
									return err
								}
								opatches = filterPatches(ranger, patches)
							}

							for _, patch := range opatches {
								reviewTxt := ""
								if patch.Review {
									reviewTxt = "[review]"
								}
								wish.Printf(
									sesh,
									"%s %s %s\n%s <%s>\n%s\n\n---\n%s\n%s\n\n\n",
									patch.Title,
									reviewTxt,
									truncateSha(patch.CommitSha),
									patch.AuthorName,
									patch.AuthorEmail,
									patch.AuthorDate,
									patch.BodyAppendix,
									patch.Body,
								)
							}

							return nil
						},
					},
					{
						Name:      "summary",
						Usage:     "List patches in PRs",
						Args:      true,
						ArgsUsage: "[prID]",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:    "filter",
								Usage:   "Only print patches in sequence range (x:y) (x:) (:y)",
								Aliases: []string{"f"},
							},
						},
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

							rnge := cCtx.String("filter")
							opatches := patches
							if rnge != "" {
								ranger, err := parseRange(rnge, len(patches))
								if err != nil {
									return err
								}
								opatches = filterPatches(ranger, patches)
							}

							w := NewTabWriter(sesh)
							fmt.Fprintln(w, "Idx\tTitle\tStatus\tCommit\tAuthor\tDate")
							for idx, patch := range opatches {
								reviewTxt := ""
								if patch.Review {
									reviewTxt = "[review]"
								}
								fmt.Fprintf(
									w,
									"%d\t%s\t%s\t%s\t%s <%s>\t%s\n",
									idx,
									patch.Title,
									reviewTxt,
									truncateSha(patch.CommitSha),
									patch.AuthorName,
									patch.AuthorEmail,
									patch.AuthorDate,
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
							prID, err := getPrID(cCtx.Args().First())
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

							err = pr.UpdatePatchRequest(prID, pubkey, "accepted")
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
							prID, err := getPrID(cCtx.Args().First())
							if err != nil {
								return err
							}
							patchReq, err := pr.GetPatchRequestByID(prID)
							if err != nil {
								return err
							}
							pk := sesh.PublicKey()
							isContrib := be.Pubkey(pk) == patchReq.Pubkey
							isAdmin := be.IsAdmin(pk)
							if !isAdmin && !isContrib {
								return fmt.Errorf("you are not authorized to change PR status")
							}

							if patchReq.Status == "closed" {
								return fmt.Errorf("PR has already been closed")
							}

							err = pr.UpdatePatchRequest(prID, pubkey, "closed")
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
							prID, err := getPrID(cCtx.Args().First())
							if err != nil {
								return err
							}
							patchReq, err := pr.GetPatchRequestByID(prID)
							if err != nil {
								return err
							}
							pk := sesh.PublicKey()
							isContrib := be.Pubkey(pk) == patchReq.Pubkey
							isAdmin := be.IsAdmin(pk)
							if !isAdmin && !isContrib {
								return fmt.Errorf("you are not authorized to change PR status")
							}

							if patchReq.Status == "open" {
								return fmt.Errorf("PR is already open")
							}

							err = pr.UpdatePatchRequest(prID, pubkey, "open")
							if err == nil {
								wish.Printf(sesh, "Reopened PR %s (#%d)\n", patchReq.Name, patchReq.ID)
							}
							return err
						},
					},
					{
						Name:  "add",
						Usage: "Append a patch to a PR",
						Flags: []cli.Flag{
							&cli.BoolFlag{
								Name:  "review",
								Usage: "mark patch as a review",
							},
							&cli.BoolFlag{
								Name:  "force",
								Usage: "replace patchset with new patchset -- including reviews",
							},
						},
						Action: func(cCtx *cli.Context) error {
							prID, err := getPrID(cCtx.Args().First())
							if err != nil {
								return err
							}
							prq, err := pr.GetPatchRequestByID(prID)
							if err != nil {
								return err
							}

							isAdmin := be.IsAdmin(sesh.PublicKey())
							isReview := cCtx.Bool("review")
							isReplace := cCtx.Bool("force")
							isPrOwner := be.IsPrOwner(prq.Pubkey, be.Pubkey(sesh.PublicKey()))
							if !isAdmin && !isPrOwner {
								return fmt.Errorf("unauthorized, you are not the owner of this PR")
							}

							op := OpNormal
							if isReview {
								wish.Println(sesh, "Marking new patchset as a review")
								op = OpReview
							} else if isReplace {
								wish.Println(sesh, "Replacing current patchset with new one")
								op = OpReplace
							}

							patches, err := pr.SubmitPatchSet(prID, pubkey, op, sesh)
							if err != nil {
								return err
							}

							if len(patches) == 0 {
								wish.Println(sesh, "Patches submitted! However none were saved, probably because they already exist in the system")
								return nil
							}

							reviewTxt := ""
							if isReview {
								err = pr.UpdatePatchRequest(prID, pubkey, "reviewed")
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
							return nil
						},
					},
				},
			},
		},
	}

	return app
}
