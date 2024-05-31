package git

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/alecthomas/chroma/v2"
	formatterHtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

//go:embed tmpl/*
var tmplFS embed.FS

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
			filepath.Join("tmpl", "pr-header.html"),
			filepath.Join("tmpl", "base.html"),
		),
	)
	return tmpl
}

type LinkData struct {
	Url  template.URL
	Text string
}

type RepoListData struct {
	Repos []LinkData
}

func repoListHandler(w http.ResponseWriter, r *http.Request) {
	web, err := getWebCtx(r)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	repos, err := web.Pr.GetRepos()
	if err != nil {
		web.Pr.Backend.Logger.Error("cannot get repos", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	repoUrls := []LinkData{}
	for _, repo := range repos {
		url := LinkData{
			Url:  template.URL("/repos/" + repo.ID),
			Text: repo.ID,
		}
		repoUrls = append(repoUrls, url)
	}

	w.Header().Set("content-type", "text/html")
	tmpl := getTemplate("repo-list.html")
	err = tmpl.Execute(w, RepoListData{
		Repos: repoUrls,
	})
	if err != nil {
		fmt.Println(err)
	}
}

type PrListData struct {
	LinkData
	ID     int64
	Pubkey string
	Date   string
	Status string
}

type RepoDetailData struct {
	ID        string
	CloneAddr string
	Prs       []PrListData
}

func repoHandler(w http.ResponseWriter, r *http.Request) {
	repoID := r.PathValue("id")

	web, err := getWebCtx(r)
	if err != nil {
		fmt.Println(err)
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

	prList := []PrListData{}
	for _, curpr := range prs {
		ls := PrListData{
			ID:     curpr.ID,
			Pubkey: curpr.Pubkey,
			LinkData: LinkData{
				Url:  template.URL(fmt.Sprintf("/prs/%d", curpr.ID)),
				Text: curpr.Name,
			},
			Date:   curpr.CreatedAt.Format(time.RFC3339),
			Status: curpr.Status,
		}
		prList = append(prList, ls)
	}

	w.Header().Set("content-type", "text/html")
	tmpl := getTemplate("repo-detail.html")
	err = tmpl.Execute(w, RepoDetailData{
		ID:        repo.ID,
		CloneAddr: repo.CloneAddr,
		Prs:       prList,
	})
	if err != nil {
		fmt.Println(err)
	}
}

type PrData struct {
	ID     int64
	Title  string
	Date   string
	Pubkey string
	Status string
}

type PatchData struct {
	*Patch
	DiffStr template.HTML
}

type PrHeaderData struct {
	Page       string
	Repo       LinkData
	Pr         PrData
	PatchesUrl template.URL
	SummaryUrl template.URL
	Patches    []PatchData
}

func prHandler(w http.ResponseWriter, r *http.Request) {
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

	patches, err := web.Pr.GetPatchesByPrID(int64(prID))
	if err != nil {
		web.Logger.Error("cannot get patches", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	patchesData := []PatchData{}
	for _, patch := range patches {
		patchesData = append(patchesData, PatchData{
			Patch:   patch,
			DiffStr: "",
		})
	}

	w.Header().Set("content-type", "text/html")
	tmpl := getTemplate("pr-detail.html")
	err = tmpl.Execute(w, PrHeaderData{
		Page: "pr",
		Repo: LinkData{
			Url:  template.URL("/repos/" + repo.ID),
			Text: repo.ID,
		},
		SummaryUrl: template.URL(fmt.Sprintf("/prs/%d", pr.ID)),
		PatchesUrl: template.URL(fmt.Sprintf("/prs/%d/patches", pr.ID)),
		Patches:    patchesData,
		Pr: PrData{
			ID:     pr.ID,
			Title:  pr.Name,
			Pubkey: pr.Pubkey,
			Date:   pr.CreatedAt.Format(time.RFC3339),
			Status: pr.Status,
		},
	})
	if err != nil {
		fmt.Println(err)
	}
}

func prPatchesHandler(w http.ResponseWriter, r *http.Request) {
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

	patches, err := web.Pr.GetPatchesByPrID(int64(prID))
	if err != nil {
		web.Pr.Backend.Logger.Error("cannot get patches", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	patchesData := []PatchData{}
	for _, patch := range patches {
		diffStr, err := parseText(web.Formatter, web.Theme, patch.RawText)
		if err != nil {
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}

		patchesData = append(patchesData, PatchData{
			Patch:   patch,
			DiffStr: template.HTML(diffStr),
		})
	}

	w.Header().Set("content-type", "text/html")
	tmpl := getTemplate("pr-detail-patches.html")
	err = tmpl.Execute(w, PrHeaderData{
		Page: "patches",
		Repo: LinkData{
			Url:  template.URL("/repos/" + repo.ID),
			Text: repo.ID,
		},
		SummaryUrl: template.URL(fmt.Sprintf("/prs/%d", pr.ID)),
		PatchesUrl: template.URL(fmt.Sprintf("/prs/%d/patches", pr.ID)),
		Patches:    patchesData,
		Pr: PrData{
			ID:     pr.ID,
			Title:  pr.Name,
			Pubkey: pr.Pubkey,
			Date:   pr.CreatedAt.Format(time.RFC3339),
			Status: pr.Status,
		},
	})
	if err != nil {
		fmt.Println(err)
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
		fmt.Println(err)
	}
}

func StartWebServer() {
	host := os.Getenv("GIT_HOST")
	if host == "" {
		host = "0.0.0.0"
	}
	port := os.Getenv("GIT_WEB_PORT")
	if port == "" {
		port = "3000"
	}
	addr := fmt.Sprintf("%s:%s", host, port)
	logger := slog.Default()

	cfg := NewGitCfg()
	dbh, err := Open(filepath.Join(cfg.DataPath, "pr.db"), logger)
	if err != nil {
		logger.Error("could not open db", "err", err)
		return
	}

	be := &Backend{
		DB:     dbh,
		Logger: logger,
		Cfg:    cfg,
	}
	prCmd := &PrCmd{
		Backend: be,
	}
	formatter := formatterHtml.New(
		formatterHtml.WithLineNumbers(true),
		formatterHtml.WithLinkableLineNumbers(true, ""),
		formatterHtml.WithClasses(true),
	)
	web := &WebCtx{
		Pr:        prCmd,
		Backend:   be,
		Logger:    logger,
		Formatter: formatter,
		Theme:     styles.Get("dracula"),
	}
	ctx := context.Background()
	ctx = setWebCtx(ctx, web)

	// ensure legacy router is disabled
	// GODEBUG=httpmuxgo121=0
	http.HandleFunc("GET /prs/{id}/patches", ctxMdw(ctx, prPatchesHandler))
	http.HandleFunc("GET /prs/{id}", ctxMdw(ctx, prHandler))
	http.HandleFunc("GET /repos/{id}", ctxMdw(ctx, repoHandler))
	http.HandleFunc("GET /", ctxMdw(ctx, repoListHandler))
	http.HandleFunc("GET /syntax.css", ctxMdw(ctx, chromaStyleHandler))

	logger.Info("starting web server", "addr", addr)
	err = http.ListenAndServe(addr, nil)
	if err != nil {
		logger.Error("listen", "err", err)
	}
}
