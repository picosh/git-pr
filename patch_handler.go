package git

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/charmbracelet/ssh"
	"github.com/picosh/send/send/utils"
)

type UploadHandler struct {
	Cfg    *GitCfg
	Logger *slog.Logger
}

func NewUploadHandler(cfg *GitCfg, logger *slog.Logger) *UploadHandler {
	return &UploadHandler{
		Cfg:    cfg,
		Logger: logger,
	}
}

func (h *UploadHandler) Read(s ssh.Session, entry *utils.FileEntry) (os.FileInfo, utils.ReaderAtCloser, error) {
	fmt.Println("read")
	cleanFilename := filepath.Base(entry.Filepath)

	if cleanFilename == "" || cleanFilename == "." {
		return nil, nil, os.ErrNotExist
	}

	return nil, nil, os.ErrNotExist
}

func (h *UploadHandler) List(s ssh.Session, fpath string, isDir bool, recursive bool) ([]os.FileInfo, error) {
	fmt.Println("list")
	var fileList []os.FileInfo
	cleanFilename := filepath.Base(fpath)

	if cleanFilename == "" || cleanFilename == "." || cleanFilename == "/" {
		name := cleanFilename
		if name == "" {
			name = "/"
		}

		fileList = append(fileList, &utils.VirtualFile{
			FName:  name,
			FIsDir: true,
		})
	} else {
	}

	return fileList, nil
}

func (h *UploadHandler) GetLogger() *slog.Logger {
	return h.Logger
}

func (h *UploadHandler) Validate(s ssh.Session) error {
	fmt.Println("validate")
	return nil
}

func (h *UploadHandler) Write(s ssh.Session, entry *utils.FileEntry) (string, error) {
	fmt.Println("write")
	logger := h.GetLogger()
	user := s.User()

	filename := filepath.Base(entry.Filepath)
	logger = logger.With(
		"user", user,
		"filepath", entry.Filepath,
		"size", entry.Size,
		"filename", filename,
	)

	var text []byte
	if b, err := io.ReadAll(entry.Reader); err == nil {
		text = b
	}

	fmt.Println(string(text))

	return "", nil
}
