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
		},
	}
}
