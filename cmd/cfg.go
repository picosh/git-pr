package cmd

import "github.com/picosh/git-pr"

func NewPicoCfg() *git.GitCfg {
	test := git.NewRepo("test", "git@github.com:picosh/test")
	test.Desc = "A test repo to play around with Patch Requests"

	pico := git.NewRepo("pico", "git@github.com:picosh/pico")
	pico.Desc = "hacker labs - open and managed web services leveraging ssh"

	pr := git.NewRepo("git-pr", "git@github.com:picosh/git-pr")
	pr.Desc = "the easiest git collaboration tool"

	ptun := git.NewRepo("ptun", "git@github.com:picosh/ptun")
	ptun.Desc = "passwordless authentication for the web"

	pobj := git.NewRepo("pobj", "git@github.com:picosh/pobj")
	pobj.Desc = "rsync, scp, sftp for your object store"

	send := git.NewRepo("send", "git@github.com:picosh/send")
	send.Desc = "ssh wish middleware for sending and receiving files from familiar tools (rsync, scp, sftp)"

	docs := git.NewRepo("docs", "git@github.com:picosh/docs")
	docs.Desc = "pico.sh doc site"

	return git.NewGitCfg(
		"ssh_data",
		"pr.pico.sh",
		[]*git.Repo{test, pico, ptun, pobj, send, docs, pr},
	)
}
