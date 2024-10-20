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
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/alecthomas/chroma/v2"
	formatterHtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/gorilla/feeds"
)

//go:embed tmpl/*
var tmplFS embed.FS

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

func getTemplate(file string) *template.Template {
	tmpl := template.Must(
		template.ParseFS(
			tmplFS,
			filepath.Join("tmpl", file),
			filepath.Join("tmpl", "patch.html"),
			filepath.Join("tmpl", "pr-header.html"),
			filepath.Join("tmpl", "pr-list-item.html"),
			filepath.Join("tmpl", "pr-table.html"),
			filepath.Join("tmpl", "pr-status.html"),
			filepath.Join("tmpl", "base.html"),
		),
	)
	return tmpl
}

type LinkData struct {
	Url  template.URL
	Text string
}

type PrTableData struct {
	Prs []*PrListData
	MetaData
}

func getPrTableData(web *WebCtx, prs []*PatchRequest) ([]*PrListData, error) {
	prdata := []*PrListData{}
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
		isAdmin := web.Backend.IsAdmin(pk)
		prls := &PrListData{
			RepoID: curpr.RepoID,
			ID:     curpr.ID,
			UserData: UserData{
				Name:    user.Name,
				IsAdmin: isAdmin,
				Pubkey:  user.Pubkey,
			},
			RepoLink: LinkData{
				Url:  template.URL(fmt.Sprintf("/repos/%s", curpr.RepoID)),
				Text: curpr.RepoID,
			},
			PrLink: LinkData{
				Url:  template.URL(fmt.Sprintf("/prs/%d", curpr.ID)),
				Text: curpr.Name,
			},
			Date:   curpr.CreatedAt.Format(web.Backend.Cfg.TimeFormat),
			Status: curpr.Status,
		}
		prdata = append(prdata, prls)
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

	prdata, err := getPrTableData(web, prs)
	if err != nil {
		web.Logger.Error("could not get pr table data", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("content-type", "text/html")
	tmpl := getTemplate("index.html")
	err = tmpl.Execute(w, PrTableData{
		Prs: prdata,
		MetaData: MetaData{
			URL: web.Backend.Cfg.Url,
		},
	})
	if err != nil {
		web.Backend.Logger.Error("cannot execute template", "err", err)
	}
}

type UserData struct {
	Name    string
	IsAdmin bool
	Pubkey  string
}

type MetaData struct {
	URL string
}

type PrListData struct {
	UserData
	RepoID   string
	RepoLink LinkData
	PrLink   LinkData
	Title    string
	ID       int64
	Date     string
	Status   string
}

type RepoDetailData struct {
	ID        string
	CloneAddr string
	Branch    string
	Desc      string
	Prs       []*PrListData
	MetaData
}

func repoDetailHandler(w http.ResponseWriter, r *http.Request) {
	repoID := r.PathValue("id")

	web, err := getWebCtx(r)
	if err != nil {
		web.Logger.Error("fetch web", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	repo, err := web.Pr.GetRepoByID(repoID)
	if err != nil {
		web.Logger.Error("fetch repo", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	prs, err := web.Pr.GetPatchRequestsByRepoID(repoID)
	if err != nil {
		web.Logger.Error("cannot get prs", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	prdata, err := getPrTableData(web, prs)
	if err != nil {
		web.Logger.Error("cannot get pr table data", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("content-type", "text/html")
	tmpl := getTemplate("repo-detail.html")
	err = tmpl.Execute(w, RepoDetailData{
		ID:        repo.ID,
		CloneAddr: repo.CloneAddr,
		Desc:      repo.Desc,
		Branch:    repo.DefaultBranch,
		Prs:       prdata,
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
	Status string
}

type PatchData struct {
	*Patch
	Url                 template.URL
	DiffStr             template.HTML
	Review              bool
	FormattedAuthorDate string
}

type EventLogData struct {
	*EventLog
	UserData
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
	Page      string
	Repo      LinkData
	Pr        PrData
	Patches   []PatchData
	Branch    string
	Logs      []EventLogData
	Patchsets []PatchsetData
	MetaData
}

func prDetailHandler(w http.ResponseWriter, r *http.Request) {
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

	pr, err := web.Pr.GetPatchRequestByID(int64(prID))
	if err != nil {
		web.Pr.Backend.Logger.Error("cannot get prs", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	repo, err := web.Pr.GetRepoByID(pr.RepoID)
	if err != nil {
		web.Logger.Error("cannot get repo", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	patchsets, err := web.Pr.GetPatchsetsByPrID(int64(prID))
	if err != nil {
		web.Logger.Error("cannot get latest patchset", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// get patchsets and diff from previous patchset
	patchsetsData := []PatchsetData{}
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

		patchsetsData = append(patchsetsData, PatchsetData{
			Patchset:    patchset,
			FormattedID: getFormattedPatchsetID(patchset.ID),
			UserData: UserData{
				Name:    user.Name,
				IsAdmin: web.Backend.IsAdmin(pk),
				Pubkey:  user.Pubkey,
			},
			Date:      patchset.CreatedAt.Format(time.RFC3339),
			RangeDiff: rangeDiff,
		})
	}

	patchesData := []PatchData{}
	if len(patchsetsData) >= 1 {
		latest := patchsetsData[len(patchsets)-1]
		patches, err := web.Pr.GetPatchesByPatchsetID(latest.ID)
		if err != nil {
			web.Logger.Error("cannot get patches", "err", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		for _, patch := range patches {
			timestamp := patch.AuthorDate.Format(web.Backend.Cfg.TimeFormat)
			diffStr, err := parseText(web.Formatter, web.Theme, patch.RawText)
			if err != nil {
				web.Logger.Error("cannot parse patch", "err", err)
				w.WriteHeader(http.StatusUnprocessableEntity)
				return
			}

			// highlight review
			isReview := false

			patchesData = append(patchesData, PatchData{
				Patch:               patch,
				Url:                 template.URL(fmt.Sprintf("patch-%d", patch.ID)),
				DiffStr:             template.HTML(diffStr),
				Review:              isReview,
				FormattedAuthorDate: timestamp,
			})
		}
	}

	user, err := web.Pr.GetUserByID(pr.UserID)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.Header().Set("content-type", "text/html")
	tmpl := getTemplate("pr-detail.html")
	pk, err := web.Backend.PubkeyToPublicKey(user.Pubkey)
	if err != nil {
		web.Logger.Error("cannot parse pubkey for pr user", "err", err)
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}
	isAdmin := web.Backend.IsAdmin(pk)
	logs, err := web.Pr.GetEventLogsByPrID(int64(prID))
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

		logData = append(logData, EventLogData{
			EventLog:            eventlog,
			FormattedPatchsetID: getFormattedPatchsetID(eventlog.PatchsetID.Int64),
			UserData: UserData{
				Name:    user.Name,
				IsAdmin: web.Backend.IsAdmin(pk),
				Pubkey:  user.Pubkey,
			},
			Date: eventlog.CreatedAt.Format(web.Backend.Cfg.TimeFormat),
		})
	}

	err = tmpl.Execute(w, PrDetailData{
		Page: "pr",
		Repo: LinkData{
			Url:  template.URL("/repos/" + repo.ID),
			Text: repo.ID,
		},
		Branch:    repo.DefaultBranch,
		Patches:   patchesData,
		Patchsets: patchsetsData,
		Logs:      logData,
		Pr: PrData{
			ID: pr.ID,
			UserData: UserData{
				Name:    user.Name,
				IsAdmin: isAdmin,
				Pubkey:  user.Pubkey,
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
	repoID := r.PathValue("repoid")
	pubkey := r.URL.Query().Get("pubkey")

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
	} else if repoID != "" {
		eventLogs, err = web.Pr.GetEventLogsByRepoID(repoID)
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
		realUrl := fmt.Sprintf("%s/prs/%d", web.Backend.Cfg.Url, eventLog.PatchRequestID.Int64)
		content := fmt.Sprintf(
			"<div><div>RepoID: %s</div><div>PatchRequestID: %d</div><div>Event: %s</div><div>Created: %s</div><div>Data: %s</div></div>",
			eventLog.RepoID,
			eventLog.PatchRequestID.Int64,
			eventLog.Event,
			eventLog.CreatedAt.Format(time.RFC3339Nano),
			eventLog.Data,
		)
		pr, err := web.Pr.GetPatchRequestByID(eventLog.PatchRequestID.Int64)
		if err != nil {
			continue
		}

		user, err := web.Pr.GetUserByID(pr.UserID)
		if err != nil {
			continue
		}

		title := fmt.Sprintf(
			`%s in %s for PR "%s" (#%d)`,
			eventLog.Event,
			eventLog.RepoID,
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

func StartWebServer(cfg *GitCfg) {
	addr := fmt.Sprintf("%s:%s", cfg.Host, cfg.WebPort)

	dbpath := filepath.Join(cfg.DataDir, "pr.db")
	dbh, err := SqliteOpen(dbpath, cfg.Logger)
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
		formatterHtml.WithClasses(true),
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
	http.HandleFunc("GET /prs/{id}", ctxMdw(ctx, prDetailHandler))
	http.HandleFunc("GET /prs/{id}/rss", ctxMdw(ctx, rssHandler))
	http.HandleFunc("GET /repos/{id}", ctxMdw(ctx, repoDetailHandler))
	http.HandleFunc("GET /repos/{repoid}/rss", ctxMdw(ctx, rssHandler))
	http.HandleFunc("GET /", ctxMdw(ctx, indexHandler))
	http.HandleFunc("GET /syntax.css", ctxMdw(ctx, chromaStyleHandler))
	http.HandleFunc("GET /rss", ctxMdw(ctx, rssHandler))
	embedFS, err := getEmbedFS(embedStaticFS, "static")
	if err != nil {
		panic(err)
	}
	userFS := getUserDefinedFS(cfg.DataDir, "static")

	http.HandleFunc("GET /static/{file}", ctxMdw(ctx, serveFile(userFS, embedFS)))

	cfg.Logger.Info("starting web server", "addr", addr)
	err = http.ListenAndServe(addr, nil)
	if err != nil {
		cfg.Logger.Error("listen", "err", err)
	}
}
