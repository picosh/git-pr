package git

import (
	"fmt"

	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
)

func GitPatchRequestMiddleware(be *Backend, pr GitPatchRequest) wish.Middleware {
	return func(next ssh.Handler) ssh.Handler {
		return func(sesh ssh.Session) {
			args := sesh.Command()
			cli := NewCli(sesh, be, pr)
			margs := append([]string{"git"}, args...)
			err := cli.Run(margs)
			if err != nil {
				be.Logger.Error("error when running cli", "err", err)
				wish.Fatalln(sesh, fmt.Errorf("err: %w", err))
				next(sesh)
				return
			}
		}
	}
}
