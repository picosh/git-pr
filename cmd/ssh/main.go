package main

import (
	git "github.com/picosh/git-pr"
	"github.com/picosh/git-pr/cmd"
)

func main() {
	git.GitSshServer(cmd.NewPicoCfg())
}
