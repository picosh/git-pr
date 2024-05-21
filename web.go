package git

import (
	"bytes"
	"context"
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

type TemplateData struct {
	Title string
	Body  template.HTML
}

func getTemplate() *template.Template {
	str := `<!doctype html>
<html lang="en">
	<head>
		<title>{{.Title}}</title>
		<link rel="stylesheet" href="https://pico.sh/smol.css" />
		<link rel="stylesheet" href="/syntax.css" />
	</head>
	<body class="container">
		{{.Body}}
	</body>
</html>`
	tmpl := template.Must(template.New("main").Parse(str))
	return tmpl
}

func repoListHandler(w http.ResponseWriter, r *http.Request) {
	web, err := getWebCtx(r)
	if err != nil {
		fmt.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	str := `<h1 class="text-2xl">repos</h1>`
	repos, err := web.Pr.GetRepos()
	if err != nil {
		web.Pr.Backend.Logger.Error("cannot get repos", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	str += "<ul>"
	for _, repo := range repos {
		str += fmt.Sprintf(
			`<li><a href="%s">%s</a></li>`,
			template.URL("/repos/"+repo.ID),
			repo.ID,
		)
	}
	str += "</ul>"

	w.Header().Set("content-type", "text/html")
	tmpl := getTemplate()
	err = tmpl.Execute(w, TemplateData{
		Title: "Repos",
		Body:  template.HTML(str),
	})
	if err != nil {
		fmt.Println(err)
	}
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

	str := fmt.Sprintf(`<h1 class="text-2xl"><a href="/">repos</a> / %s</h1>`, repo.ID)
	str += `<div class="group">`
	str += fmt.Sprintf(`<div><code>git clone %s</code></div>`, repo.CloneAddr)
	str += fmt.Sprintf(`<div>Submit patch request: <code>git format-patch --stdout | ssh pr.pico.sh pr create %s</code></div>`, repo.ID)
	str += `<div class="group"></div>`

	prs, err := web.Pr.GetPatchRequestsByRepoID(repoID)
	if err != nil {
		web.Logger.Error("cannot get prs", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if len(prs) == 0 {
		str += "No PRs found for repo"
	}

	for _, curpr := range prs {
		row := `
<div class="group-h">
	<div>%d</div>
	<div><a href="%s">%s</a></div>
	<div>%s</div>
</div>`
		str += fmt.Sprintf(
			row,
			curpr.ID,
			template.URL(fmt.Sprintf("/prs/%d", curpr.ID)),
			curpr.Name,
			curpr.Pubkey,
		)
	}

	w.Header().Set("content-type", "text/html")
	tmpl := getTemplate()
	err = tmpl.Execute(w, TemplateData{
		Title: "Patch Requests",
		Body:  template.HTML(str),
	})
	if err != nil {
		fmt.Println(err)
	}
}

func header(repo *Repo, pr *PatchRequest, page string) string {
	str := fmt.Sprintf(`<h1 class="text-2xl"><a href="/repos/%s">%s</a> / %s</h1>`, repo.ID, repo.ID, pr.Name)
	str += fmt.Sprintf("<div>[%s] %s %s</div>", pr.Status, pr.CreatedAt.Format(time.DateTime), pr.Pubkey)
	if page == "pr" {
		str += fmt.Sprintf(`<div><strong>summary</strong> &middot; <a href="/prs/%d/patches">patches</a></div>`, pr.ID)
	} else {
		str += fmt.Sprintf(`<div><a href="/prs/%d">summary</a> &middot; <strong>patches</strong></div>`, pr.ID)
	}

	str += fmt.Sprintf(`<div>Submit change to patch: <code>git format-patch HEAD~1 --stdout | ssh pr.pico.sh pr add %d</code></div>`, pr.ID)
	return str
}

func prHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	prID, err := strconv.Atoi(id)
	if err != nil {
		fmt.Println(err)
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}

	web, err := getWebCtx(r)
	if err != nil {
		fmt.Println(err)
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

	str := header(repo, pr, "pr")
	str += fmt.Sprintf("<p>%s</p>", pr.Text)

	patches, err := web.Pr.GetPatchesByPrID(int64(prID))
	if err != nil {
		web.Logger.Error("cannot get patches", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	for _, patch := range patches {
		reviewTxt := ""
		if patch.Review {
			reviewTxt = "[review]"
		}
		str += fmt.Sprintf(
			"<div>%s\t%s\t%s\t%s <%s>\t%s\n</div>",
			reviewTxt,
			patch.Title,
			truncateSha(patch.CommitSha),
			patch.AuthorName,
			patch.AuthorEmail,
			patch.AuthorDate.Format(time.RFC3339Nano),
		)
	}

	w.Header().Set("content-type", "text/html")
	tmpl := getTemplate()
	err = tmpl.Execute(w, TemplateData{
		Title: fmt.Sprintf("%s (%s)", pr.Name, pr.Status),
		Body:  template.HTML(str),
	})
	if err != nil {
		fmt.Println(err)
	}
}

func prPatchesHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	prID, err := strconv.Atoi(id)
	if err != nil {
		fmt.Println(err)
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}

	web, err := getWebCtx(r)
	if err != nil {
		fmt.Println(err)
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

	str := header(repo, pr, "patches")

	patches, err := web.Pr.GetPatchesByPrID(int64(prID))
	if err != nil {
		web.Pr.Backend.Logger.Error("cannot get patches", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	for _, patch := range patches {
		rev := ""
		if patch.Review {
			rev = "[review]"
		}
		diffStr, err := parseText(web.Formatter, web.Theme, patch.RawText)
		if err != nil {
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}

		row := `
<h2 class="text-xl">%s %s</h2>
<div>%s</div>`
		str += fmt.Sprintf(
			row,
			rev, patch.Title,
			diffStr,
		)
	}

	w.Header().Set("content-type", "text/html")
	tmpl := getTemplate()
	err = tmpl.Execute(w, TemplateData{
		Title: fmt.Sprintf("patches - %s", pr.Name),
		Body:  template.HTML(str),
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
