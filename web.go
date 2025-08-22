package git

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/alecthomas/chroma/v2"
	formatterHtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/bluekeyes/go-gitdiff/gitdiff"
	"github.com/gorilla/feeds"
)

var (
	//go:embed tmpl/*
	tmplFS    embed.FS
	indexTmpl = getTemplate("index.html")
	prTmpl    = getTemplate("pr.html")
	userTmpl  = getTemplate("user.html")
	repoTmpl  = getTemplate("repo.html")
)

func getTemplate(page string) *template.Template {
	tmpl, err := template.New("").Funcs(template.FuncMap{
		"sha": shaFn,
	}).ParseFS(
		tmplFS,
		filepath.Join("tmpl", "pages", page),
		filepath.Join("tmpl", "components", "*.html"),
		filepath.Join("tmpl", "base.html"),
	)
	if err != nil {
		panic(err)
	}
	return tmpl.Lookup(page)
}

//go:embed static/*
var embedStaticFS embed.FS

type WebCtx struct {
	Pr        *PrCmd
	Backend   *Backend
	Formatter *formatterHtml.Formatter
	Logger    *slog.Logger
	Theme     *chroma.Style
}

type ctxWeb struct{}

func getWebCtx(r *http.Request) (*WebCtx, error) {
	data, ok := r.Context().Value(ctxWeb{}).(*WebCtx)
	if data == nil || !ok {
		return data, fmt.Errorf("webCtx not set on `r.Context()` for connection")
	}
	return data, nil
}

func setWebCtx(ctx context.Context, web *WebCtx) context.Context {
	return context.WithValue(ctx, ctxWeb{}, web)
}

// converts contents of files in git tree to pretty formatted code.
func parseText(formatter *formatterHtml.Formatter, theme *chroma.Style, text string) (string, error) {
	lexer := lexers.Get("diff")
	iterator, err := lexer.Tokenise(nil, text)
	if err != nil {
		return text, err
	}
	var buf bytes.Buffer
	err = formatter.Format(&buf, theme, iterator)
	if err != nil {
		return text, err
	}
	return buf.String(), nil
}

func ctxMdw(ctx context.Context, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handler(w, r.WithContext(ctx))
	}
}

func shaFn(sha string) string {
	if sha == "" {
		return "(none)"
	}
	return truncateSha(sha)
}

type LinkData struct {
	Url  template.URL
	Text string
}

type BasicData struct {
	MetaData
}

type PrTableData struct {
	Prs         []*PrListData
	NumOpen     int
	NumAccepted int
	NumClosed   int
	MetaData
}

type UserDetailData struct {
	Prs         []*PrListData
	UserData    UserData
	NumOpen     int
	NumAccepted int
	NumClosed   int
	MetaData
}

type RepoDetailData struct {
	Name        string
	UserID      int64
	Username    string
	Branch      string
	Prs         []*PrListData
	NumOpen     int
	NumAccepted int
	NumClosed   int
	MetaData
}

func createPrDataSorter(sort, sortDir string) func(a, b *PrListData) int {
	return func(a *PrListData, b *PrListData) int {
		if sort == "status" {
			statusA := strings.ToLower(string(a.Status))
			statusB := strings.ToLower(string(b.Status))
			if sortDir == "asc" {
				return strings.Compare(statusA, statusB)
			} else {
				return strings.Compare(statusB, statusA)
			}
		}

		if sort == "title" {
			titleA := strings.ToLower(a.Title)
			titleB := strings.ToLower(b.Title)
			if sortDir == "asc" {
				return strings.Compare(titleA, titleB)
			} else {
				return strings.Compare(titleB, titleA)
			}
		}

		if sort == "repo" {
			repoA := strings.ToLower(a.RepoNs)
			repoB := strings.ToLower(b.RepoNs)
			if sortDir == "asc" {
				return strings.Compare(repoA, repoB)
			} else {
				return strings.Compare(repoB, repoA)
			}
		}

		if sort == "created_at" {
			if sortDir == "asc" {
				return a.DateOrig.Compare(b.DateOrig)
			} else {
				return b.DateOrig.Compare(a.DateOrig)
			}
		}

		if sortDir == "desc" {
			return int(b.ID) - int(a.ID)
		}
		return int(a.ID) - int(b.ID)
	}
}

func getPrTableData(web *WebCtx, prs []*PatchRequest, query url.Values) ([]*PrListData, error) {
	prdata := []*PrListData{}
	status := Status(strings.ToLower(query.Get("status")))
	if status == "" {
		status = StatusOpen
	}
	username := strings.ToLower(query.Get("user"))
	title := strings.ToLower(query.Get("title"))
	sort := strings.ToLower(query.Get("sort"))
	sortDir := strings.ToLower(query.Get("sort_dir"))
	hasFilter := status != "" || username != "" || title != ""

	for _, curpr := range prs {
		user, err := web.Pr.GetUserByID(curpr.UserID)
		if err != nil {
			web.Logger.Error("cannot get user from pr", "err", err)
			continue
		}
		pk, err := web.Backend.PubkeyToPublicKey(user.Pubkey)
		if err != nil {
			web.Logger.Error("cannot get pubkey from user public key", "err", err)
			continue
		}

		repo, err := web.Pr.GetRepoByID(curpr.RepoID)
		if err != nil {
			web.Logger.Error("cannot get repo", "err", err)
			continue
		}

		repoUser, err := web.Pr.GetUserByID(repo.UserID)
		if err != nil {
			web.Logger.Error("cannot get repo user", "err", err)
			continue
		}

		ps, err := web.Pr.GetPatchsetsByPrID(curpr.ID)
		if err != nil {
			web.Logger.Error("cannot get patchsets for pr", "err", err)
			continue
		}

		if hasFilter {
			if status != "" {
				if status != curpr.Status {
					continue
				}
			}

			if username != "" {
				if username != strings.ToLower(user.Name) {
					continue
				}
			}

			if title != "" {
				if !strings.Contains(strings.ToLower(curpr.Name), title) {
					continue
				}
			}
		}

		isAdmin := web.Backend.IsAdmin(pk)
		repoNs := web.Backend.CreateRepoNs(repoUser.Name, repo.Name)
		prls := &PrListData{
			RepoNs: repoNs,
			ID:     curpr.ID,
			UserData: UserData{
				Name:    user.Name,
				IsAdmin: isAdmin,
				Pubkey:  user.Pubkey,
			},
			RepoLink: LinkData{
				Url:  template.URL(fmt.Sprintf("/r/%s/%s", repoUser.Name, repo.Name)),
				Text: repoNs,
			},
			PrLink: LinkData{
				Url:  template.URL(fmt.Sprintf("/prs/%d", curpr.ID)),
				Text: curpr.Name,
			},
			NumPatchsets: len(ps),
			DateOrig:     curpr.CreatedAt,
			Date:         curpr.CreatedAt.Format(web.Backend.Cfg.TimeFormat),
			Status:       curpr.Status,
		}
		prdata = append(prdata, prls)
	}

	if sort != "" {
		if sortDir == "" {
			sortDir = "asc"
		}
		slices.SortFunc(prdata, createPrDataSorter(sort, sortDir))
	}

	return prdata, nil
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	web, err := getWebCtx(r)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	prs, err := web.Pr.GetPatchRequests()
	if err != nil {
		web.Logger.Error("could not get prs", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	prdata, err := getPrTableData(web, prs, r.URL.Query())
	if err != nil {
		web.Logger.Error("could not get pr table data", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	numOpen := 0
	numAccepted := 0
	numClosed := 0
	for _, pr := range prs {
		switch pr.Status {
		case "open":
			numOpen += 1
		case "accepted":
			numAccepted += 1
		case "closed":
			numClosed += 1
		}
	}

	w.Header().Set("content-type", "text/html")
	err = indexTmpl.Execute(w, PrTableData{
		NumOpen:     numOpen,
		NumAccepted: numAccepted,
		NumClosed:   numClosed,
		Prs:         prdata,
		MetaData: MetaData{
			URL:  web.Backend.Cfg.Url,
			Desc: template.HTML(web.Backend.Cfg.Desc),
		},
	})
	if err != nil {
		web.Backend.Logger.Error("cannot execute template", "err", err)
	}
}

type UserData struct {
	UserID    int64
	Name      string
	IsAdmin   bool
	Pubkey    string
	CreatedAt string
}

type MetaData struct {
	URL  string
	Desc template.HTML
}

type PrListData struct {
	UserData
	RepoNs       string
	RepoLink     LinkData
	PrLink       LinkData
	Title        string
	NumPatchsets int
	ID           int64
	DateOrig     time.Time
	Date         string
	Status       Status
}

func userDetailHandler(w http.ResponseWriter, r *http.Request) {
	userName := r.PathValue("user")

	web, err := getWebCtx(r)
	if err != nil {
		web.Logger.Error("fetch web", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	user, err := web.Pr.GetUserByName(userName)
	if err != nil {
		web.Logger.Error("cannot find user by name", "err", err, "name", userName)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	pk, err := web.Backend.PubkeyToPublicKey(user.Pubkey)
	if err != nil {
		web.Logger.Error("cannot parse pubkey for pr user", "err", err)
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}
	isAdmin := web.Backend.IsAdmin(pk)

	prs, err := web.Pr.GetPatchRequestsByPubkey(user.Pubkey)
	if err != nil {
		web.Logger.Error("cannot get prs", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	query := r.URL.Query()
	query.Set("user", userName)

	prdata, err := getPrTableData(web, prs, query)
	if err != nil {
		web.Logger.Error("cannot get pr table data", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	numOpen := 0
	numAccepted := 0
	numClosed := 0
	for _, pr := range prs {
		switch pr.Status {
		case "open":
			numOpen += 1
		case "accepted":
			numAccepted += 1
		case "closed":
			numClosed += 1
		}
	}

	w.Header().Set("content-type", "text/html")
	err = userTmpl.Execute(w, UserDetailData{
		Prs:         prdata,
		NumOpen:     numOpen,
		NumAccepted: numAccepted,
		NumClosed:   numClosed,
		UserData: UserData{
			UserID:    user.ID,
			Name:      user.Name,
			Pubkey:    user.Pubkey,
			CreatedAt: user.CreatedAt.Format(time.RFC3339),
			IsAdmin:   isAdmin,
		},
		MetaData: MetaData{
			URL: web.Backend.Cfg.Url,
		},
	})
	if err != nil {
		web.Backend.Logger.Error("cannot execute template", "err", err)
	}
}

func repoDetailHandler(w http.ResponseWriter, r *http.Request) {
	userName := r.PathValue("user")
	repoName := r.PathValue("repo")

	web, err := getWebCtx(r)
	if err != nil {
		web.Logger.Error("fetch web", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	user, err := web.Pr.GetUserByName(userName)
	if err != nil {
		web.Logger.Error("cannot find user", "user", user, "err", err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	repo, err := web.Pr.GetRepoByName(user, repoName)
	if err != nil {
		web.Logger.Error("cannot find repo", "user", user, "err", err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	prs, err := web.Pr.GetPatchRequestsByRepoID(repo.ID)
	if err != nil {
		web.Logger.Error("cannot get prs", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	prdata, err := getPrTableData(web, prs, r.URL.Query())
	if err != nil {
		web.Logger.Error("cannot get pr table data", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	numOpen := 0
	numAccepted := 0
	numClosed := 0
	for _, pr := range prs {
		switch pr.Status {
		case "open":
			numOpen += 1
		case "accepted":
			numAccepted += 1
		case "closed":
			numClosed += 1
		}
	}

	w.Header().Set("content-type", "text/html")
	err = repoTmpl.Execute(w, RepoDetailData{
		Name:        repo.Name,
		UserID:      user.ID,
		Username:    userName,
		Prs:         prdata,
		NumOpen:     numOpen,
		NumAccepted: numAccepted,
		NumClosed:   numClosed,
		MetaData: MetaData{
			URL: web.Backend.Cfg.Url,
		},
	})
	if err != nil {
		web.Backend.Logger.Error("cannot execute template", "err", err)
	}
}

type PrData struct {
	UserData
	ID     int64
	Title  string
	Date   string
	Status Status
}

type PatchFile struct {
	*gitdiff.File
	Adds     int64
	Dels     int64
	DiffText template.HTML
}

type PatchData struct {
	*Patch
	PatchFiles          []*PatchFile
	PatchHeader         *gitdiff.PatchHeader
	Url                 template.URL
	Review              bool
	FormattedAuthorDate string
}

type EventLogData struct {
	*EventLog
	UserData
	*Patchset
	FormattedPatchsetID string
	Date                string
}

type PatchsetData struct {
	*Patchset
	UserData
	FormattedID string
	Date        string
	RangeDiff   []*RangeDiffOutput
}

type PrDetailData struct {
	Page         string
	Repo         LinkData
	Pr           PrData
	Patchset     *Patchset
	PatchsetData *PatchsetData
	Patches      []PatchData
	Branch       string
	Logs         []EventLogData
	Patchsets    []PatchsetData
	IsRangeDiff  bool
	MetaData
}

func createPrDetail(page string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		prID, err := strconv.Atoi(id)
		if err != nil {
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}

		web, err := getWebCtx(r)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		var pr *PatchRequest
		var ps *Patchset
		switch page {
		case "pr":
			{
				pr, err = web.Pr.GetPatchRequestByID(int64(prID))
				if err != nil {
					web.Pr.Backend.Logger.Error("cannot get prs", "err", err)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
			}
		case "ps":
		case "rd":
			{
				ps, err = web.Pr.GetPatchsetByID(int64(prID))
				if err != nil {
					web.Pr.Backend.Logger.Error("cannot get patchset", "err", err)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}

				pr, err = web.Pr.GetPatchRequestByID(int64(ps.PatchRequestID))
				if err != nil {
					web.Pr.Backend.Logger.Error("cannot get pr", "err", err)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
			}
		}

		patchsets, err := web.Pr.GetPatchsetsByPrID(pr.ID)
		if err != nil {
			web.Logger.Error("cannot get latest patchset", "err", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// get patchsets and diff from previous patchset
		patchsetsData := []PatchsetData{}
		var selectedPatchsetData *PatchsetData
		for idx, patchset := range patchsets {
			user, err := web.Pr.GetUserByID(patchset.UserID)
			if err != nil {
				web.Logger.Error("could not get user for patch", "err", err)
				continue
			}

			var prevPatchset *Patchset
			if idx > 0 {
				prevPatchset = patchsets[idx-1]
			}

			var rangeDiff []*RangeDiffOutput
			if idx > 0 {
				rangeDiff, err = web.Pr.DiffPatchsets(prevPatchset, patchset)
				if err != nil {
					web.Logger.Error("could not diff patchset", "err", err)
					continue
				}
			}

			pk, err := web.Backend.PubkeyToPublicKey(user.Pubkey)
			if err != nil {
				web.Logger.Error("cannot parse pubkey for pr user", "err", err)
				w.WriteHeader(http.StatusUnprocessableEntity)
				return
			}

			// set selected patchset to latest when no ps already set
			if ps == nil && idx == len(patchsets)-1 {
				ps = patchset
			}

			data := PatchsetData{
				Patchset:    patchset,
				FormattedID: getFormattedPatchsetID(patchset.ID),
				UserData: UserData{
					UserID:    user.ID,
					Name:      user.Name,
					IsAdmin:   web.Backend.IsAdmin(pk),
					Pubkey:    user.Pubkey,
					CreatedAt: user.CreatedAt.Format(time.RFC3339),
				},
				Date:      patchset.CreatedAt.Format(time.RFC3339),
				RangeDiff: rangeDiff,
			}
			patchsetsData = append(patchsetsData, data)
			if ps != nil && ps.ID == patchset.ID {
				selectedPatchsetData = &data
			}
		}

		patchesData := []PatchData{}
		if len(patchsetsData) >= 1 {
			psID := ps.ID
			patches, err := web.Pr.GetPatchesByPatchsetID(psID)
			if err != nil {
				web.Logger.Error("cannot get patches", "err", err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}

			// TODO: a little hacky
			reviewIDs := []int64{}
			for _, data := range patchsetsData {
				if psID != data.ID {
					continue
				}
				if !data.Review {
					continue
				}

				for _, rdiff := range data.RangeDiff {
					if rdiff.Type == "add" {
						for _, patch := range patches {
							commSha := truncateSha(patch.CommitSha)
							if strings.Contains(rdiff.Header.String(), commSha) {
								reviewIDs = append(reviewIDs, patch.ID)
								break
							}
						}
					}
				}
				break
			}

			for _, patch := range patches {
				diffFiles, preamble, err := ParsePatch(patch.RawText)
				if err != nil {
					web.Logger.Error("cannot parse patch", "err", err)
					w.WriteHeader(http.StatusUnprocessableEntity)
					return
				}
				header, err := gitdiff.ParsePatchHeader(preamble)
				if err != nil {
					web.Logger.Error("cannot parse patch", "err", err)
					w.WriteHeader(http.StatusUnprocessableEntity)
					return
				}

				// highlight review
				isReview := slices.Contains(reviewIDs, patch.ID)

				patchFiles := []*PatchFile{}
				for _, file := range diffFiles {
					var adds int64 = 0
					var dels int64 = 0
					for _, frag := range file.TextFragments {
						adds += frag.LinesAdded
						dels += frag.LinesDeleted
					}

					diffStr, err := parseText(web.Formatter, web.Theme, file.String())
					if err != nil {
						web.Logger.Error("cannot parse patch", "err", err)
						w.WriteHeader(http.StatusUnprocessableEntity)
						return
					}

					patchFiles = append(patchFiles, &PatchFile{
						File:     file,
						Adds:     adds,
						Dels:     dels,
						DiffText: template.HTML(diffStr),
					})
				}

				timestamp := patch.AuthorDate.Format(web.Backend.Cfg.TimeFormat)
				patchesData = append(patchesData, PatchData{
					Patch:               patch,
					Url:                 template.URL(fmt.Sprintf("patch-%d", patch.ID)),
					Review:              isReview,
					FormattedAuthorDate: timestamp,
					PatchFiles:          patchFiles,
					PatchHeader:         header,
				})
			}
		}

		user, err := web.Pr.GetUserByID(pr.UserID)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.Header().Set("content-type", "text/html")
		pk, err := web.Backend.PubkeyToPublicKey(user.Pubkey)
		if err != nil {
			web.Logger.Error("cannot parse pubkey for pr user", "err", err)
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		isAdmin := web.Backend.IsAdmin(pk)
		logs, err := web.Pr.GetEventLogsByPrID(pr.ID)
		if err != nil {
			web.Logger.Error("cannot get logs for pr", "err", err)
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		slices.SortFunc(logs, func(a *EventLog, b *EventLog) int {
			return a.CreatedAt.Compare(b.CreatedAt)
		})

		logData := []EventLogData{}
		for _, eventlog := range logs {
			user, _ := web.Pr.GetUserByID(eventlog.UserID)
			pk, err := web.Backend.PubkeyToPublicKey(user.Pubkey)
			if err != nil {
				web.Logger.Error("cannot parse pubkey for pr user", "err", err)
				w.WriteHeader(http.StatusUnprocessableEntity)
				return
			}
			var logps *Patchset
			if eventlog.PatchsetID.Int64 > 0 {
				logps, err = web.Pr.GetPatchsetByID(eventlog.PatchsetID.Int64)
				if err != nil {
					web.Logger.Error("cannot get patchset", "err", err, "ps", eventlog.PatchsetID)
					w.WriteHeader(http.StatusUnprocessableEntity)
					return
				}
			}

			logData = append(logData, EventLogData{
				EventLog:            eventlog,
				FormattedPatchsetID: getFormattedPatchsetID(eventlog.PatchsetID.Int64),
				Patchset:            logps,
				UserData: UserData{
					UserID:    user.ID,
					Name:      user.Name,
					IsAdmin:   web.Backend.IsAdmin(pk),
					Pubkey:    user.Pubkey,
					CreatedAt: user.CreatedAt.Format(time.RFC3339),
				},
				Date: eventlog.CreatedAt.Format(web.Backend.Cfg.TimeFormat),
			})
		}

		repo, err := web.Pr.GetRepoByID(pr.RepoID)
		if err != nil {
			web.Logger.Error("cannot get repo for pr", "err", err)
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}

		repoOwner, err := web.Pr.GetUserByID(repo.UserID)
		if err != nil {
			web.Logger.Error("cannot get repo for pr", "err", err)
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}

		repoNs := web.Backend.CreateRepoNs(repoOwner.Name, repo.Name)
		url := fmt.Sprintf("/r/%s/%s", repoOwner.Name, repo.Name)
		err = prTmpl.Execute(w, PrDetailData{
			Page: "pr",
			Repo: LinkData{
				Url:  template.URL(url),
				Text: repoNs,
			},
			Branch:       "main",
			Patchset:     ps,
			PatchsetData: selectedPatchsetData,
			IsRangeDiff:  page == "rd",
			Patches:      patchesData,
			Patchsets:    patchsetsData,
			Logs:         logData,
			Pr: PrData{
				ID: pr.ID,
				UserData: UserData{
					UserID:    user.ID,
					Name:      user.Name,
					IsAdmin:   isAdmin,
					Pubkey:    user.Pubkey,
					CreatedAt: user.CreatedAt.Format(time.RFC3339),
				},
				Title:  pr.Name,
				Date:   pr.CreatedAt.Format(web.Backend.Cfg.TimeFormat),
				Status: pr.Status,
			},
			MetaData: MetaData{
				URL: web.Backend.Cfg.Url,
			},
		})
		if err != nil {
			web.Backend.Logger.Error("cannot execute template", "err", err)
		}
	}
}

func rssHandler(w http.ResponseWriter, r *http.Request) {
	web, err := getWebCtx(r)
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}

	desc := fmt.Sprintf(
		"Events related to git collaboration server %s",
		web.Backend.Cfg.Url,
	)
	feed := &feeds.Feed{
		Title:       fmt.Sprintf("%s events", web.Backend.Cfg.Url),
		Link:        &feeds.Link{Href: web.Backend.Cfg.Url},
		Description: desc,
		Author:      &feeds.Author{Name: "git collaboration server"},
		Created:     time.Now(),
	}

	var eventLogs []*EventLog
	id := r.PathValue("id")
	pubkey := r.URL.Query().Get("pubkey")
	username := r.PathValue("user")
	repoName := r.PathValue("repo")

	if id != "" {
		var prID int64
		prID, err = getPrID(id)
		if err != nil {
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		eventLogs, err = web.Pr.GetEventLogsByPrID(prID)
	} else if pubkey != "" {
		user, perr := web.Pr.GetUserByPubkey(pubkey)
		if perr != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		eventLogs, err = web.Pr.GetEventLogsByUserID(user.ID)
	} else if username != "" {
		user, perr := web.Pr.GetUserByName(username)
		if perr != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		eventLogs, err = web.Pr.GetEventLogsByUserID(user.ID)
	} else if repoName != "" {
		user, perr := web.Pr.GetUserByName(username)
		if perr != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		eventLogs, err = web.Pr.GetEventLogsByRepoName(user, repoName)
	} else {
		eventLogs, err = web.Pr.GetEventLogs()
	}

	if err != nil {
		web.Logger.Error("rss could not get eventLogs", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	var feedItems []*feeds.Item
	for _, eventLog := range eventLogs {
		user, err := web.Pr.GetUserByID(eventLog.UserID)
		if err != nil {
			web.Logger.Error("user not found for event log", "id", eventLog.ID, "err", err)
			continue
		}

		repo := &Repo{Name: "unknown"}
		if eventLog.RepoID.Valid {
			repo, err = web.Pr.GetRepoByID(eventLog.RepoID.Int64)
			if err != nil {
				web.Logger.Error("repo not found for event log", "id", eventLog.ID, "err", err)
				continue
			}
		}

		realUrl := fmt.Sprintf("%s/prs/%d", web.Backend.Cfg.Url, eventLog.PatchRequestID.Int64)
		content := fmt.Sprintf(
			"<div><div>RepoID: %s</div><div>PatchRequestID: %d</div><div>Event: %s</div><div>Created: %s</div><div>Data: %s</div></div>",
			web.Backend.CreateRepoNs(user.Name, repo.Name),
			eventLog.PatchRequestID.Int64,
			eventLog.Event,
			eventLog.CreatedAt.Format(time.RFC3339Nano),
			eventLog.Data,
		)
		pr, err := web.Pr.GetPatchRequestByID(eventLog.PatchRequestID.Int64)
		if err != nil {
			continue
		}

		title := fmt.Sprintf(
			`%s in %s for PR "%s" (#%d)`,
			eventLog.Event,
			web.Backend.CreateRepoNs(user.Name, repo.Name),
			pr.Name,
			eventLog.PatchRequestID.Int64,
		)
		item := &feeds.Item{
			Id:          fmt.Sprintf("%d", eventLog.ID),
			Title:       title,
			Link:        &feeds.Link{Href: realUrl},
			Content:     content,
			Created:     eventLog.CreatedAt,
			Description: title,
			Author:      &feeds.Author{Name: user.Name},
		}

		feedItems = append(feedItems, item)
	}
	feed.Items = feedItems

	rss, err := feed.ToAtom()
	if err != nil {
		web.Logger.Error("could not generate atom rss feed", "err", err)
		http.Error(w, "Could not generate atom rss feed", http.StatusInternalServerError)
	}

	w.Header().Add("Content-Type", "application/atom+xml; charset=utf-8")
	_, err = w.Write([]byte(rss))
	if err != nil {
		web.Logger.Error("write error atom rss feed", "err", err)
	}
}

func chromaStyleHandler(w http.ResponseWriter, r *http.Request) {
	web, err := getWebCtx(r)
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}
	w.Header().Add("content-type", "text/css")
	err = web.Formatter.WriteCSS(w, web.Theme)
	if err != nil {
		web.Backend.Logger.Error("cannot write css file", "err", err)
	}
}

func serveFile(userfs fs.FS, embedfs fs.FS) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		web, err := getWebCtx(r)
		if err != nil {
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		logger := web.Logger

		file := r.PathValue("file")

		logger.Info("serving file", "file", file)
		// merging both embedded fs and whatever user provides
		var reader fs.File
		if userfs == nil {
			reader, err = embedfs.Open(file)
		} else {
			reader, err = userfs.Open(file)
			if err != nil {
				// serve embeded static folder
				reader, err = embedfs.Open(file)
			}
		}

		if err != nil {
			logger.Error(err.Error())
			http.Error(w, "file not found", 404)
			return
		}

		contents, err := io.ReadAll(reader)
		if err != nil {
			logger.Error(err.Error())
			http.Error(w, "file not found", 404)
			return
		}
		contentType := mime.TypeByExtension(filepath.Ext(file))
		if contentType == "" {
			contentType = http.DetectContentType(contents)
		}
		w.Header().Add("Content-Type", contentType)

		_, err = w.Write(contents)
		if err != nil {
			logger.Error(err.Error())
			http.Error(w, "server error", 500)
			return
		}
	}
}

func getUserDefinedFS(datadir, dirName string) fs.FS {
	dir := filepath.Join(datadir, dirName)
	_, err := os.Stat(dir)
	if err != nil {
		return nil
	}
	return os.DirFS(dir)
}

func getEmbedFS(ffs embed.FS, dirName string) (fs.FS, error) {
	fsys, err := fs.Sub(ffs, dirName)
	if err != nil {
		return nil, err
	}
	return fsys, nil
}

func GitWebServer(cfg *GitCfg) http.Handler {
	dbpath := filepath.Join(cfg.DataDir, "pr.db?_fk=on")
	dbh, err := SqliteOpen("file:"+dbpath, cfg.Logger)
	if err != nil {
		panic(fmt.Sprintf("cannot find database file, check folder and perms: %s: %s", dbpath, err))
	}

	be := &Backend{
		DB:     dbh,
		Logger: cfg.Logger,
		Cfg:    cfg,
	}
	prCmd := &PrCmd{
		Backend: be,
	}
	formatter := formatterHtml.New(
		formatterHtml.WithLineNumbers(true),
		formatterHtml.LineNumbersInTable(true),
		formatterHtml.WithClasses(true),
		formatterHtml.WithLinkableLineNumbers(true, "gitpr"),
	)
	web := &WebCtx{
		Pr:        prCmd,
		Backend:   be,
		Logger:    cfg.Logger,
		Formatter: formatter,
		Theme:     styles.Get(cfg.Theme),
	}

	ctx := context.Background()
	ctx = setWebCtx(ctx, web)

	// ensure legacy router is disabled
	// GODEBUG=httpmuxgo121=0
	mux := http.NewServeMux()
	mux.HandleFunc("GET /prs/{id}", ctxMdw(ctx, createPrDetail("pr")))
	mux.HandleFunc("GET /prs/{id}/rss", ctxMdw(ctx, rssHandler))
	mux.HandleFunc("GET /ps/{id}", ctxMdw(ctx, createPrDetail("ps")))
	mux.HandleFunc("GET /rd/{id}", ctxMdw(ctx, createPrDetail("rd")))
	mux.HandleFunc("GET /r/{user}/{repo}/rss", ctxMdw(ctx, rssHandler))
	mux.HandleFunc("GET /r/{user}/{repo}", ctxMdw(ctx, repoDetailHandler))
	mux.HandleFunc("GET /r/{user}", ctxMdw(ctx, userDetailHandler))
	mux.HandleFunc("GET /rss/{user}", ctxMdw(ctx, rssHandler))
	mux.HandleFunc("GET /rss", ctxMdw(ctx, rssHandler))
	mux.HandleFunc("GET /", ctxMdw(ctx, indexHandler))
	mux.HandleFunc("GET /syntax.css", ctxMdw(ctx, chromaStyleHandler))
	embedFS, err := getEmbedFS(embedStaticFS, "static")
	if err != nil {
		panic(err)
	}
	userFS := getUserDefinedFS(cfg.DataDir, "static")

	mux.HandleFunc("GET /static/{file}", ctxMdw(ctx, serveFile(userFS, embedFS)))
	return mux
}
