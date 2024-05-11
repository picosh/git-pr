package git

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/ssh"
)

func getAuthorizedKeys(path string) ([]ssh.PublicKey, error) {
	keys := []ssh.PublicKey{}
	f, err := os.Open(path)
	if err != nil {
		return keys, err
	}
	defer f.Close() // nolint: errcheck

	rd := bufio.NewReader(f)
	for {
		line, _, err := rd.ReadLine()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return keys, err
		}
		if strings.TrimSpace(string(line)) == "" {
			continue
		}
		if bytes.HasPrefix(line, []byte{'#'}) {
			continue
		}
		upk, _, _, _, err := ssh.ParseAuthorizedKey(line)
		if err != nil {
			return keys, err
		}
		keys = append(keys, upk)
	}

	return keys, nil
}
