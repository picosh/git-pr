package git

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
	"github.com/charmbracelet/ssh"
)

var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
var startOfPatch = "From "

// https://stackoverflow.com/a/22892986
func randSeq(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func truncateSha(sha string) string {
	return sha[:7]
}

func getAuthorizedKeys(pubkeys []string) ([]ssh.PublicKey, error) {
	keys := []ssh.PublicKey{}
	for _, pubkey := range pubkeys {
		if strings.TrimSpace(pubkey) == "" {
			continue
		}
		if strings.HasPrefix(pubkey, "#") {
			continue
		}
		upk, _, _, _, err := ssh.ParseAuthorizedKey([]byte(pubkey))
		if err != nil {
			return keys, err
		}
		keys = append(keys, upk)
	}

	return keys, nil
}

type Ranger struct {
	Left  int
	Right int
}

func parseRange(rnge string, sliceLen int) (*Ranger, error) {
	items := strings.Split(rnge, ":")
	left := 0
	var err error
	if items[0] != "" {
		left, err = strconv.Atoi(items[0])
		if err != nil {
			return nil, fmt.Errorf("first value before `:` must provide number")
		}
	}

	if left < 0 {
		return nil, fmt.Errorf("first value must be >= 0")
	}

	if left >= sliceLen {
		return nil, fmt.Errorf("first value must be less than number of patches")
	}

	if len(items) == 1 {
		return &Ranger{
			Left:  left,
			Right: left,
		}, nil
	}

	if items[1] == "" {
		return &Ranger{Left: left, Right: sliceLen - 1}, nil
	}

	right, err := strconv.Atoi(items[1])
	if err != nil {
		return nil, fmt.Errorf("second value after `:` must provide number")
	}

	if left > right {
		return nil, fmt.Errorf("second value must be greater than first value")
	}

	if right >= sliceLen {
		return nil, fmt.Errorf("second value must be less than number of patches")
	}

	return &Ranger{
		Left:  left,
		Right: right,
	}, nil
}

func filterPatches(ranger *Ranger, patches []*Patch) []*Patch {
	if ranger.Left == ranger.Right {
		return []*Patch{patches[ranger.Left]}
	}
	return patches[ranger.Left:ranger.Right]
}

func splitPatchSet(patchset string) []string {
	return strings.Split(patchset, "\n"+startOfPatch)
}

func findBaseCommit(patch string) string {
	re := regexp.MustCompile(`base-commit: (.+)\s*`)
	strs := re.FindStringSubmatch(patch)
	baseCommit := ""
	if len(strs) > 1 {
		baseCommit = strs[1]
	}
	return baseCommit
}

func parsePatchSet(patchset io.Reader) ([]*Patch, error) {
	patches := []*Patch{}
	buf := new(strings.Builder)
	_, err := io.Copy(buf, patchset)
	if err != nil {
		return nil, err
	}

	patchesRaw := splitPatchSet(buf.String())
	for idx, patchRaw := range patchesRaw {
		patchStr := patchRaw
		if idx > 0 {
			patchStr = startOfPatch + patchRaw
		}
		reader := strings.NewReader(patchStr)
		diffFiles, preamble, err := gitdiff.Parse(reader)
		if err != nil {
			return nil, err
		}
		header, err := gitdiff.ParsePatchHeader(preamble)
		if err != nil {
			return nil, err
		}

		baseCommit := findBaseCommit(patchRaw)
		authorName := "Unknown"
		authorEmail := ""
		if header.Author != nil {
			authorName = header.Author.Name
			authorEmail = header.Author.Email
		}

		if len(diffFiles) == 0 {
			continue
		}

		contentSha := calcContentSha(diffFiles, header)

		patches = append(patches, &Patch{
			AuthorName:    authorName,
			AuthorEmail:   authorEmail,
			AuthorDate:    header.AuthorDate.UTC().String(),
			Title:         header.Title,
			Body:          header.Body,
			BodyAppendix:  header.BodyAppendix,
			CommitSha:     header.SHA,
			ContentSha:    contentSha,
			RawText:       patchRaw,
			BaseCommitSha: sql.NullString{String: baseCommit},
		})
	}

	return patches, nil
}

// calcContentSha calculates a shasum containing the important content
// changes related to a patch.
// We cannot rely on patch.CommitSha because it includes the commit date
// that will change when a user fetches and applies the patch locally.
func calcContentSha(diffFiles []*gitdiff.File, header *gitdiff.PatchHeader) string {
	authorName := ""
	authorEmail := ""
	if header.Author != nil {
		authorName = header.Author.Name
		authorEmail = header.Author.Email
	}
	content := fmt.Sprintf(
		"%s\n%s\n%s\n%s\n%s\n",
		header.Title,
		header.Body,
		authorName,
		authorEmail,
		header.AuthorDate,
	)
	for _, diff := range diffFiles {
		dff := fmt.Sprintf(
			"%s->%s %s..%s %s->%s\n",
			diff.OldName, diff.NewName,
			diff.OldOIDPrefix, diff.NewOIDPrefix,
			diff.OldMode.String(), diff.NewMode.String(),
		)
		content += dff
	}
	sha := sha256.Sum256([]byte(content))
	shaStr := hex.EncodeToString(sha[:])
	return shaStr
}

func AuthorDateToTime(date string, logger *slog.Logger) time.Time {
	// TODO: convert sql column to DATETIME
	ds, err := time.Parse("2006-01-02T15:04:05Z", date)
	if err != nil {
		logger.Error(
			"cannot parse author date for patch",
			"datetime", date,
			"err", err,
		)
		return time.Now()
	}
	return ds
}

/* func gitServiceCommands(sesh ssh.Session, be *Backend, cmd, repoName string) error {
	name := utils.SanitizeRepo(repoName)
	// git bare repositories should end in ".git"
	// https://git-scm.com/docs/gitrepository-layout
	repoID := be.RepoID(name)
	reposDir := be.ReposDir()
	err := git.EnsureWithin(reposDir, repoID)
	if err != nil {
		return err
	}
	repoPath := filepath.Join(reposDir, repoID)
	serviceCmd := git.ServiceCommand{
		Stdin:  sesh,
		Stdout: sesh,
		Stderr: sesh.Stderr(),
		Dir:    repoPath,
		Env:    sesh.Environ(),
	}

	if cmd == "git-receive-pack" {
		err := git.ReceivePack(sesh.Context(), serviceCmd)
		if err != nil {
			return err
		}
	} else if cmd == "git-upload-pack" {
		err := git.UploadPack(sesh.Context(), serviceCmd)
		if err != nil {
			return err
		}
	}

	return nil
} */
