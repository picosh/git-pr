package git

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/charmbracelet/ssh"
	"github.com/knadh/koanf/parsers/toml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

type Repo struct {
	ID            string `koanf:"id"`
	Desc          string `koanf:"desc"`
	CloneAddr     string `koanf:"clone_addr"`
	DefaultBranch string `koanf:"default_branch"`
}

var k = koanf.New(".")

type GitCfg struct {
	DataDir   string          `koanf:"data_dir"`
	Repos     []*Repo         `koanf:"repo"`
	Url       string          `koanf:"url"`
	Host      string          `koanf:"host"`
	SshPort   string          `koanf:"ssh_port"`
	WebPort   string          `koanf:"web_port"`
	AdminsStr []string        `koanf:"admins"`
	Admins    []ssh.PublicKey `koanf:"admins_pk"`
	Theme     string          `koanf:"theme"`
	Logger    *slog.Logger
}

func NewGitCfg(fpath string, logger *slog.Logger) *GitCfg {
	logger.Info("loading configuration file", "fpath", fpath)

	if err := k.Load(file.Provider(fpath), toml.Parser()); err != nil {
		panic(fmt.Sprintf("error loading config: %v", err))
	}

	err := k.Load(env.Provider("GITPR_", ".", func(s string) string {
		keyword := strings.ToLower(strings.TrimPrefix(s, "GITPR_"))
		return keyword
	}), nil)
	if err != nil {
		panic(fmt.Sprintf("could not load environment variables: %v", err))
	}

	var out GitCfg
	err = k.UnmarshalWithConf("", &out, koanf.UnmarshalConf{Tag: "koanf"})
	if err != nil {
		panic(fmt.Sprintf("could not unmarshal config: %v", err))
	}

	if len(out.AdminsStr) > 0 {
		keys, err := getAuthorizedKeys(out.AdminsStr)
		if err == nil {
			out.Admins = keys
		} else {
			panic(fmt.Sprintf("could not parse authorized keys file: %v", err))
		}
	} else {
		logger.Info("no admin specified in config so no one can submit a review!")
	}

	if out.DataDir == "" {
		out.DataDir = "data"
	}

	if out.Host == "" {
		out.Host = "0.0.0.0"
	}

	if out.SshPort == "" {
		out.SshPort = "2222"
	}

	if out.WebPort == "" {
		out.WebPort = "3000"
	}

	if out.Theme == "" {
		out.Theme = "dracula"
	}

	logger.Info(
		"config",
		"url", out.Url,
		"data_dir", out.DataDir,
		"host", out.Host,
		"ssh_port", out.SshPort,
		"web_port", out.WebPort,
		"theme", out.Theme,
	)

	for _, pubkey := range out.AdminsStr {
		logger.Info("admin", "pubkey", pubkey)
	}

	for _, repo := range out.Repos {
		if repo.DefaultBranch == "" {
			repo.DefaultBranch = "main"
		}
		logger.Info(
			"repo",
			"id", repo.ID,
			"desc", repo.Desc,
			"clone_addr", repo.CloneAddr,
			"default_branch", repo.DefaultBranch,
		)
	}

	out.Logger = logger
	return &out
}
