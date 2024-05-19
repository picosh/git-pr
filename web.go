package git

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
)

type ctxPr struct{}

func getPrCtx(r *http.Request) (*PrCmd, error) {
	pr, ok := r.Context().Value(ctxPr{}).(*PrCmd)
	if pr == nil || !ok {
		return pr, fmt.Errorf("pr not set on `r.Context()` for connection")
	}
	return pr, nil
}
func setPrCtx(ctx context.Context, pr *PrCmd) context.Context {
	return context.WithValue(ctx, ctxPr{}, pr)
}

func ctxMdw(ctx context.Context, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handler(w, r.WithContext(ctx))
	}
}

func prHandler(w http.ResponseWriter, r *http.Request) {
	pr, err := getPrCtx(r)
	if err != nil {
		fmt.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	str := "Patch Requests\n"
	prs, err := pr.GetPatchRequests()
	if err != nil {
		pr.Backend.Logger.Error("cannot get prs", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	for _, curpr := range prs {
		str += fmt.Sprintf("%d\t%s\t%s\t%s\n", curpr.ID, curpr.RepoID, curpr.Name, curpr.Pubkey)
	}
	fmt.Fprintf(w, str)
}

func StartWebServer() {
	host := os.Getenv("WEB_HOST")
	if host == "" {
		host = "0.0.0.0"
	}
	port := os.Getenv("WEB_PORT")
	if port == "" {
		port = "3000"
	}
	addr := fmt.Sprintf("%s:%s", host, port)
	logger := slog.Default()

	dbh, err := Open("./test.db", logger)
	if err != nil {
		logger.Error("could not open db", "err", err)
		return
	}

	be := &Backend{
		DB:     dbh,
		Logger: logger,
	}
	prCmd := &PrCmd{
		Backend: be,
	}
	ctx := context.Background()
	ctx = setPrCtx(ctx, prCmd)

	mux := http.NewServeMux()
	mux.HandleFunc("/", ctxMdw(ctx, prHandler))

	logger.Info("starting web server", "addr", addr)
	err = http.ListenAndServe(addr, mux)
	if err != nil {
		logger.Error("listen", "err", err)
	}
}
