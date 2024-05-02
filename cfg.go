package git

type GitCfg struct {
	DataPath string
}

func NewGitCfg() *GitCfg {
	return &GitCfg{
		DataPath: "ssh_data",
	}
}
