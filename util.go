package git

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"math/rand"
	"regexp"
	"strconv"
	"strings"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
	"github.com/charmbracelet/ssh"
)

var baseCommitRe = regexp.MustCompile(`base-commit: (.+)\s*`)
var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
var startOfPatch = "From "
var patchsetPrefix = "ps-"
var prPrefix = "pr-"

// https://stackoverflow.com/a/22892986
func randSeq(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return strings.ToLower(string(b))
}

func truncateSha(sha string) string {
	if len(sha) < 7 {
		return sha
	}
	return sha[:7]
}

func GetAuthorizedKeys(pubkeys []string) ([]ssh.PublicKey, error) {
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

func getFormattedPatchsetID(id int64) string {
	if id == 0 {
		return ""
	}
	return fmt.Sprintf("%s%d", patchsetPrefix, id)
}

func getPrID(prID string) (int64, error) {
	recID, err := strconv.Atoi(strings.Replace(prID, prPrefix, "", 1))
	if err != nil {
		return 0, err
	}
	return int64(recID), nil
}

func getPatchsetID(patchsetID string) (int64, error) {
	psID, err := strconv.Atoi(strings.Replace(patchsetID, patchsetPrefix, "", 1))
	if err != nil {
		return 0, err
	}
	return int64(psID), nil
}

func splitPatchSet(patchset string) []string {
	return strings.Split(patchset, "\n"+startOfPatch)
}

func findBaseCommit(patch string) string {
	strs := baseCommitRe.FindStringSubmatch(patch)
	baseCommit := ""
	if len(strs) > 1 {
		baseCommit = strs[1]
	}
	return baseCommit
}

func patchToDiff(patch io.Reader) (string, error) {
	by, err := io.ReadAll(patch)
	if err != nil {
		return "", err
	}
	str := string(by)
	idx := strings.Index(str, "diff --git")
	if idx == -1 {
		return "", fmt.Errorf("no diff found in patch")
	}
	trailIdx := strings.LastIndex(str, "-- \n")
	if trailIdx >= 0 {
		return str[idx:trailIdx], nil
	}
	return str[idx:], nil
}

func ParsePatch(patchRaw string) ([]*gitdiff.File, string, error) {
	reader := strings.NewReader(patchRaw)
	diffFiles, preamble, err := gitdiff.Parse(reader)
	return diffFiles, preamble, err
}

func ParsePatchset(patchset io.Reader) ([]*Patch, error) {
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
		diffFiles, preamble, err := ParsePatch(patchStr)
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

		contentSha := calcContentSha(diffFiles, header)

		patches = append(patches, &Patch{
			AuthorName:    authorName,
			AuthorEmail:   authorEmail,
			AuthorDate:    header.AuthorDate.UTC(),
			Title:         header.Title,
			Body:          header.Body,
			BodyAppendix:  header.BodyAppendix,
			CommitSha:     header.SHA,
			ContentSha:    contentSha,
			RawText:       patchStr,
			BaseCommitSha: sql.NullString{String: baseCommit},
			Files:         diffFiles,
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
		"%s\n%s\n%s\n%s\n",
		header.Title,
		header.Body,
		authorName,
		authorEmail,
	)
	for _, diff := range diffFiles {
		// we need to ignore diffs with base commit because that depends
		// on the client that is exporting the patch
		foundBase := false
		for _, text := range diff.TextFragments {
			for _, line := range text.Lines {
				base := findBaseCommit(line.Line)
				if base != "" {
					foundBase = true
				}
			}
		}

		if foundBase {
			continue
		}

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
