package git

type GitCfg struct {
	DataPath     string
	AdminPubkeys []string
}

func NewGitCfg() *GitCfg {
	return &GitCfg{
		DataPath:     "ssh_data",
		AdminPubkeys: []string{},
	}
}
