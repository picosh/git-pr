package git

import "github.com/charmbracelet/ssh"

type GitCfg struct {
	DataPath string
	Admins   []ssh.PublicKey
}

func NewGitCfg() *GitCfg {
	return &GitCfg{
		DataPath: "ssh_data",
	}
}
