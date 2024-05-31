package git

import "github.com/charmbracelet/ssh"

type Repo struct {
	ID        string
	Desc      string
	CloneAddr string
}

type GitCfg struct {
	DataPath string
	Admins   []ssh.PublicKey
	Repos    []Repo
}

func NewGitCfg() *GitCfg {
	return &GitCfg{
		DataPath: "./ssh_data",
		Repos: []Repo{
			{
				ID:        "test",
				Desc:      "A test repo to play around with Patch Requests",
				CloneAddr: "git@github.com:picosh/test",
			},
			{
				ID:        "pico",
				Desc:      "hacker labs - open and managed web services leveraging ssh",
				CloneAddr: "git@github.com:picosh/pico",
			},
			{
				ID:        "ptun",
				Desc:      "passwordless authentication for the web",
				CloneAddr: "git@github.com:picosh/ptun",
			},
		},
	}
}
