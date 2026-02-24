package git

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/ssh"
	"github.com/knadh/koanf/parsers/toml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

var k = koanf.New(".")

type GitCfg struct {
	DataDir    string          `koanf:"data_dir"`
	Url        string          `koanf:"url"`
	Host       string          `koanf:"host"`
	SshPort    string          `koanf:"ssh_port"`
	WebPort    string          `koanf:"web_port"`
	PromPort   string          `koanf:"prom_port"`
	AdminsStr  []string        `koanf:"admins"`
	Admins     []ssh.PublicKey `koanf:"admins_pk"`
	CreateRepo string          `koanf:"create_repo"`
	Theme      string          `koanf:"theme"`
	TimeFormat string          `koanf:"time_format"`
	Desc       string          `koanf:"desc"`
	Logger     *slog.Logger
}

func LoadConfigFile(fpath string, logger *slog.Logger) {
	fpp, err := filepath.Abs(fpath)
	if err != nil {
		panic(err)
	}
	logger.Info("loading configuration file", "fpath", fpp)

	if err := k.Load(file.Provider(fpp), toml.Parser()); err != nil {
		panic(fmt.Sprintf("error loading config: %v", err))
	}
}

func NewGitCfg(logger *slog.Logger) *GitCfg {
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
		keys, err := GetAuthorizedKeys(out.AdminsStr)
		if err == nil {
			out.Admins = keys
		} else {
			panic(fmt.Sprintf("could not parse authorized keys file: %v", err))
		}
	} else {
		logger.Info("no admin specified in config so no one can submit a review!")
	}

	// make datadir absolute
	tmpdir := out.DataDir
	if out.DataDir == "" {
		tmpdir = "./data"
	}
	datadir, err := filepath.Abs(tmpdir)
	out.DataDir = datadir
	if err != nil {
		panic(err)
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

	if out.TimeFormat == "" {
		out.TimeFormat = time.RFC3339
	}

	if out.CreateRepo == "" {
		out.CreateRepo = "admin"
	}

	logger.Info(
		"config",
		"url", out.Url,
		"data_dir", out.DataDir,
		"host", out.Host,
		"ssh_port", out.SshPort,
		"web_port", out.WebPort,
		"theme", out.Theme,
		"time_format", out.TimeFormat,
		"create_repo", out.CreateRepo,
		"desc", out.Desc,
	)

	for _, pubkey := range out.AdminsStr {
		logger.Info("admin", "pubkey", pubkey)
	}

	out.Logger = logger
	return &out
}
