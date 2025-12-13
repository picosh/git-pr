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

var (
	COST_MAX                           = 65536
	RANGE_DIFF_CREATION_FACTOR_DEFAULT = 60
)

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
			files := outputRemovedPatch(patchA)
			outputs = append(
				outputs,
				&RangeDiffOutput{
					Header: hdr,
					Type:   "rm",
					Order:  i + 1,
					Files:  files,
				},
			)
		}
	}

	for j, patchB := range b {
		if patchB.Matching == -1 {
			hdr := NewRangeDiffHeader(nil, patchB, -1, j+1)
			files := outputAddedPatch(patchB)
			outputs = append(
				outputs,
				&RangeDiffOutput{
					Header: hdr,
					Type:   "add",
					Order:  j + 1,
					Files:  files,
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
		outerDiffType := line.Type

		fmtLine := strings.Split(line.Text, "\n")
		for idx, ln := range fmtLine {
			text := ln
			if idx < len(fmtLine)-1 {
				text = ln + "\n"
			}

			// Determine inner type based on line prefix (+/-/space)
			inner := "equal"
			if strings.HasPrefix(text, "+") {
				inner = "insert"
			} else if strings.HasPrefix(text, "-") {
				inner = "delete"
			}

			// Determine outer type based on diff result
			outer := "equal"
			switch outerDiffType {
			case diffmatchpatch.DiffInsert:
				outer = "insert"
			case diffmatchpatch.DiffDelete:
				outer = "delete"
			}

			st := RangeDiffDiff{
				Text:      text,
				OuterType: outer,
				InnerType: inner,
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

// extractChangedLines extracts only added and deleted lines from a file's fragments,
// ignoring context lines. This is used for comparing patches where context lines
// may differ due to rebasing but the actual changes are the same.
func extractChangedLines(file *gitdiff.File) string {
	var result strings.Builder
	for _, frag := range file.TextFragments {
		for _, line := range frag.Lines {
			if line.Op == gitdiff.OpAdd || line.Op == gitdiff.OpDelete {
				result.WriteString(line.String())
			}
		}
	}
	return result.String()
}

// extractAllLines extracts all lines (including context) from a file's fragments.
// This is used for displaying the full diff with context.
func extractAllLines(file *gitdiff.File) string {
	var result strings.Builder
	for _, frag := range file.TextFragments {
		for _, line := range frag.Lines {
			result.WriteString(line.String())
		}
	}
	return result.String()
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
				// Compare only +/- lines to determine if there's a meaningful diff
				changedA := extractChangedLines(fileA)
				changedB := extractChangedLines(fileB)
				if changedA == changedB {
					// No difference in actual changes, skip this file
					continue
				}
				// Use all lines (with context) for display
				strA := extractAllLines(fileA)
				strB := extractAllLines(fileB)
				curDiff := DoDiff(strA, strB)
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
			strA := extractAllLines(fileA)
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
			strB := extractAllLines(fileB)
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

func outputAddedPatch(patch *PatchRange) []*RangeDiffFile {
	diffs := []*RangeDiffFile{}
	for _, file := range patch.Files {
		strB := extractAllLines(file)
		fp := &RangeDiffFile{
			OldFile: nil,
			NewFile: file,
			Diff:    DoDiff("", strB),
		}
		diffs = append(diffs, fp)
	}
	return diffs
}

func outputRemovedPatch(patch *PatchRange) []*RangeDiffFile {
	diffs := []*RangeDiffFile{}
	for _, file := range patch.Files {
		strA := extractAllLines(file)
		fp := &RangeDiffFile{
			OldFile: file,
			NewFile: nil,
			Diff:    DoDiff(strA, ""),
		}
		diffs = append(diffs, fp)
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
			fileName := ""
			if f.NewFile != nil {
				fileName = f.NewFile.NewName
			} else if f.OldFile != nil {
				fileName = f.OldFile.NewName
			}
			output += fmt.Sprintf("\n@@ %s\n", fileName)
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
