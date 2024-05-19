package git

import (
	"git.sr.ht/~rockorager/vaxis"
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
				wish.Fatalln(sesh, err)
				return
			}
		}
	}
}

func VaxisMiddleware(vx *vaxis.Vaxis) wish.Middleware {
	return func(next ssh.Handler) ssh.Handler {
		return func(sesh ssh.Session) {
			pty, windowChanges, ok := sesh.Pty()
			if !ok {
				next(sesh)
				return
			}

			win := vx.Window()
			win.Width = pty.Window.Width
			win.Height = pty.Window.Height

			go func() {
				for {
					select {
					case w := <-windowChanges:
						win.Width = w.Width
						win.Height = w.Height
						vx.Resize()
					}
				}
			}()

			for ev := range vx.Events() {
				switch ev := ev.(type) {
				case vaxis.Key:
					switch ev.String() {
					case "Ctrl+c":
						return
					}
				}

				win := vx.Window()
				win.Width = pty.Window.Width
				win.Height = pty.Window.Height
				win.Clear()
				win.Print(vaxis.Segment{Text: "Hello, World!"})
				vx.Render()
			}

			vx.Close()
		}
	}
}
