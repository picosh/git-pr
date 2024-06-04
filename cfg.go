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
	Url      string
}

func NewGitCfg() *GitCfg {
	return &GitCfg{
		DataPath: "./ssh_data",
		Url:      "pr.pico.sh",
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
			{
				ID:        "pobj",
				Desc:      "rsync, scp, sftp for your object store",
				CloneAddr: "git@github.com:picosh/ptun",
			},
			{
				ID:        "send",
				Desc:      "ssh wish middleware for sending and receiving files from familiar tools (rsync, scp, sftp)",
				CloneAddr: "git@github.com:picosh/send",
			},
			{
				ID:        "docs",
				Desc:      "pico.sh doc site",
				CloneAddr: "git@github.com:picosh/docs",
			},
		},
	}
}
