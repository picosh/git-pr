package git

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
	ha "github.com/oddg/hungarian-algorithm"
	"github.com/sergi/go-diff/diffmatchpatch"
)

var COST_MAX = 65536
var RANGE_DIFF_CREATION_FACTOR_DEFAULT = 60

type PatchRange struct {
	*Patch
	Matching int
	Diff     string
	DiffSize int
	Shown    bool
}

func NewPatchRange(patch *Patch) *PatchRange {
	diff := patch.CalcDiff()
	return &PatchRange{
		Patch:    patch,
		Matching: -1,
		Diff:     diff,
		DiffSize: len(diff),
		Shown:    false,
	}
}

type RangeDiffOutput struct {
	Header *RangeDiffHeader
	Order  int
	Files  []*RangeDiffFile
	Type   string
}

func output(a []*PatchRange, b []*PatchRange) []*RangeDiffOutput {
	outputs := []*RangeDiffOutput{}
	for i, patchA := range a {
		if patchA.Matching == -1 {
			hdr := NewRangeDiffHeader(patchA, nil, i+1, -1)
			outputs = append(
				outputs,
				&RangeDiffOutput{
					Header: hdr,
					Type:   "rm",
					Order:  i + 1,
				},
			)
		}
	}

	for j, patchB := range b {
		if patchB.Matching == -1 {
			hdr := NewRangeDiffHeader(nil, patchB, -1, j+1)
			outputs = append(
				outputs,
				&RangeDiffOutput{
					Header: hdr,
					Type:   "add",
					Order:  j + 1,
				},
			)
			continue
		}
		patchA := a[patchB.Matching]
		if patchB.ContentSha == patchA.ContentSha {
			hdr := NewRangeDiffHeader(patchA, patchB, patchB.Matching+1, patchA.Matching+1)
			outputs = append(
				outputs,
				&RangeDiffOutput{
					Header: hdr,
					Type:   "equal",
					Order:  patchA.Matching + 1,
				},
			)
		} else {
			hdr := NewRangeDiffHeader(patchA, patchB, patchB.Matching+1, patchA.Matching+1)
			diff := outputDiff(patchA, patchB)
			outputs = append(
				outputs,
				&RangeDiffOutput{
					Order:  patchA.Matching + 1,
					Header: hdr,
					Files:  diff,
					Type:   "diff",
				},
			)
		}
	}
	sort.Slice(outputs, func(i, j int) bool {
		return outputs[i].Order < outputs[j].Order
	})
	return outputs
}

type RangeDiffDiff struct {
	OuterType string
	InnerType string
	Text      string
}

func toRangeDiffDiff(diff []diffmatchpatch.Diff) []RangeDiffDiff {
	result := []RangeDiffDiff{}

	for _, line := range diff {
		outer := "equal"
		switch line.Type {
		case diffmatchpatch.DiffInsert:
			outer = "insert"
		case diffmatchpatch.DiffDelete:
			outer = "delete"
		}

		fmtLine := strings.Split(line.Text, "\n")
		for idx, ln := range fmtLine {
			text := ln
			if idx < len(fmtLine)-1 {
				text = ln + "\n"
			}
			st := RangeDiffDiff{
				Text:      text,
				OuterType: outer,
				InnerType: "equal",
			}

			if strings.HasPrefix(text, "+") {
				st.InnerType = "insert"
			} else if strings.HasPrefix(text, "-") {
				st.InnerType = "delete"
			}

			result = append(result, st)
		}
	}

	return result
}

func DoDiff(src, dst string) []RangeDiffDiff {
	dmp := diffmatchpatch.New()
	wSrc, wDst, warray := dmp.DiffLinesToChars(src, dst)
	diffs := dmp.DiffMain(wSrc, wDst, false)
	diffs = dmp.DiffCharsToLines(diffs, warray)
	return toRangeDiffDiff(diffs)
}

type RangeDiffFile struct {
	OldFile *gitdiff.File
	NewFile *gitdiff.File
	Diff    []RangeDiffDiff
}

func outputDiff(patchA, patchB *PatchRange) []*RangeDiffFile {
	diffs := []*RangeDiffFile{}

	for _, fileA := range patchA.Files {
		found := false
		for _, fileB := range patchB.Files {
			if fileA.NewName == fileB.NewName {
				found = true
				// this means both files have been deleted so we should skip
				if fileA.NewName == "" {
					continue
				}
				strA := ""
				for _, frag := range fileA.TextFragments {
					for _, line := range frag.Lines {
						strA += line.String()
					}
				}
				strB := ""
				for _, frag := range fileB.TextFragments {
					for _, line := range frag.Lines {
						strB += line.String()
					}
				}
				curDiff := DoDiff(strA, strB)
				hasDiff := false
				for _, dd := range curDiff {
					if dd.OuterType != "equal" {
						hasDiff = true
						break
					}
				}
				if !hasDiff {
					continue
				}
				fp := &RangeDiffFile{
					OldFile: fileA,
					NewFile: fileB,
					Diff:    curDiff,
				}
				diffs = append(diffs, fp)
			}
		}

		// find files in patchA but not in patchB
		if !found {
			strA := ""
			for _, frag := range fileA.TextFragments {
				for _, line := range frag.Lines {
					strA += line.String()
				}
			}
			fp := &RangeDiffFile{
				OldFile: fileA,
				NewFile: nil,
				Diff:    DoDiff(strA, ""),
			}
			diffs = append(diffs, fp)
		}
	}

	// find files in patchB not in patchA
	for _, fileB := range patchB.Files {
		found := false
		for _, fileA := range patchA.Files {
			if fileA.NewName == fileB.NewName {
				found = true
				break
			}
		}

		if !found {
			strB := ""
			for _, frag := range fileB.TextFragments {
				for _, line := range frag.Lines {
					strB += line.String()
				}
			}
			fp := &RangeDiffFile{
				OldFile: nil,
				NewFile: fileB,
				Diff:    DoDiff("", strB),
			}
			diffs = append(diffs, fp)
		}
	}

	return diffs
}

// RangeDiffHeader is a header combining old and new change pairs.
type RangeDiffHeader struct {
	OldIdx       int
	OldSha       string
	NewIdx       int
	NewSha       string
	Title        string
	ContentEqual bool
}

func NewRangeDiffHeader(a *PatchRange, b *PatchRange, aIndex, bIndex int) *RangeDiffHeader {
	hdr := &RangeDiffHeader{}
	if a == nil {
		hdr.NewIdx = bIndex
		hdr.NewSha = b.CommitSha
		hdr.Title = b.Title
		return hdr
	}
	if b == nil {
		hdr.OldIdx = aIndex
		hdr.OldSha = a.CommitSha
		hdr.Title = a.Title
		return hdr
	}

	hdr.OldIdx = aIndex
	hdr.NewIdx = bIndex
	hdr.OldSha = a.CommitSha
	hdr.NewSha = b.CommitSha

	if a.ContentSha == b.ContentSha {
		hdr.Title = a.Title
		hdr.ContentEqual = true
	} else {
		hdr.Title = b.Title
	}

	return hdr
}

func (hdr *RangeDiffHeader) String() string {
	if hdr.OldIdx == 0 {
		return fmt.Sprintf("-:  ------- > %d:  %s %s\n", hdr.NewIdx, truncateSha(hdr.NewSha), hdr.Title)
	}
	if hdr.NewIdx == 0 {
		return fmt.Sprintf("%d:  %s < -:  ------- %s\n", hdr.OldIdx, truncateSha(hdr.OldSha), hdr.Title)
	}
	if hdr.ContentEqual {
		return fmt.Sprintf(
			"%d:  %s = %d:  %s %s\n",
			hdr.OldIdx, truncateSha(hdr.OldSha),
			hdr.NewIdx, truncateSha(hdr.NewSha),
			hdr.Title,
		)
	}
	return fmt.Sprintf(
		"%d:  %s ! %d:  %s %s",
		hdr.OldIdx, truncateSha(hdr.OldSha),
		hdr.NewIdx, truncateSha(hdr.NewSha),
		hdr.Title,
	)
}

func RangeDiff(a []*Patch, b []*Patch) []*RangeDiffOutput {
	aPatches := []*PatchRange{}
	for _, patch := range a {
		aPatches = append(aPatches, NewPatchRange(patch))
	}
	bPatches := []*PatchRange{}
	for _, patch := range b {
		bPatches = append(bPatches, NewPatchRange(patch))
	}
	findExactMatches(aPatches, bPatches)
	getCorrespondences(aPatches, bPatches, RANGE_DIFF_CREATION_FACTOR_DEFAULT)
	return output(aPatches, bPatches)
}

func RangeDiffToStr(diffs []*RangeDiffOutput) string {
	output := ""
	for _, diff := range diffs {
		output += diff.Header.String()
		for _, f := range diff.Files {
			output += fmt.Sprintf("\n@@ %s\n", f.NewFile.NewName)
			for _, d := range f.Diff {
				switch d.OuterType {
				case "equal":
					output += d.Text
				case "insert":
					output += d.Text
				case "delete":
					output += d.Text
				}
			}
		}
	}
	return output
}

func findExactMatches(a []*PatchRange, b []*PatchRange) {
	for i, patchA := range a {
		for j, patchB := range b {
			if patchA.ContentSha == patchB.ContentSha {
				patchA.Matching = j
				patchB.Matching = i
			}
		}
	}
}

func createMatrix(rows, cols int) [][]int {
	mat := make([][]int, rows)
	for i := range mat {
		mat[i] = make([]int, cols)
	}
	return mat
}

func diffsize(a *PatchRange, b *PatchRange) int {
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(a.Diff, b.Diff, false)
	return len(diffs)
}

func getCorrespondences(a []*PatchRange, b []*PatchRange, creationFactor int) {
	n := len(a) + len(b)
	cost := createMatrix(n, n)

	for i, patchA := range a {
		var c int
		for j, patchB := range b {
			if patchA.Matching == j {
				c = 0
			} else if patchA.Matching == -1 && patchB.Matching == -1 {
				c = diffsize(patchA, patchB)
			} else {
				c = COST_MAX
			}
			cost[i][j] = c
		}
	}

	for j, patchB := range b {
		creationCost := (patchB.DiffSize * creationFactor) / 100
		if patchB.Matching >= 0 {
			creationCost = math.MaxInt32
		}
		for i := len(a); i < n; i++ {
			cost[i][j] = creationCost
		}
	}

	for i := len(a); i < n; i++ {
		for j := len(b); j < n; j++ {
			cost[i][j] = 0
		}
	}

	assignment, _ := ha.Solve(cost)
	for i := range a {
		j := assignment[i]
		if j >= 0 && j < len(b) {
			a[i].Matching = j
			b[j].Matching = i
		}
	}
}
