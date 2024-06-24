package git

import (
	"os"

	"github.com/charmbracelet/ssh"
)

type Repo struct {
	ID            string
	Desc          string
	CloneAddr     string
	DefaultBranch string
}

func NewRepo(id, cloneAddr string) *Repo {
	return &Repo{
		ID:            id,
		CloneAddr:     cloneAddr,
		DefaultBranch: "main",
	}
}

type GitCfg struct {
	DataPath string
	Admins   []ssh.PublicKey
	Repos    []*Repo
	Url      string
	Host     string
	SshPort  string
	WebPort  string
}

func NewGitCfg(dataPath, url string, repos []*Repo) *GitCfg {
	host := os.Getenv("GIT_HOST")
	if host == "" {
		host = "0.0.0.0"
	}

	sshPort := os.Getenv("GIT_SSH_PORT")
	if sshPort == "" {
		sshPort = "2222"
	}

	webPort := os.Getenv("GIT_WEB_PORT")
	if webPort == "" {
		webPort = "3000"
	}

	return &GitCfg{
		DataPath: dataPath,
		Url:      url,
		Repos:    repos,
		Host:     host,
		SshPort:  sshPort,
		WebPort:  webPort,
	}
}
