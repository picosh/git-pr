package git

import (
	"fmt"
	"math"

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
	Header string
	Diff   []diffmatchpatch.Diff
	Type   string
}

func output(a []*PatchRange, b []*PatchRange) []*RangeDiffOutput {
	outputs := []*RangeDiffOutput{}
	for i, patchA := range a {
		if patchA.Matching == -1 {
			outputs = append(
				outputs,
				&RangeDiffOutput{
					Header: outputPairHeader(patchA, nil, i+1, -1),
					Type:   "rm",
				},
			)
		}
	}

	for j, patchB := range b {
		if patchB.Matching == -1 {
			outputs = append(
				outputs,
				&RangeDiffOutput{
					Header: outputPairHeader(nil, patchB, -1, j+1),
					Type:   "add",
				},
			)
			continue
		}
		patchA := a[patchB.Matching]
		if patchB.ContentSha == patchA.ContentSha {
			outputs = append(
				outputs,
				&RangeDiffOutput{
					Header: outputPairHeader(patchA, patchB, patchB.Matching+1, patchA.Matching+1),
					Type:   "equal",
				},
			)
		} else {
			header := fmt.Sprintf(
				"%d:  %s ! %d:  %s %s",
				patchA.Matching+1, truncateSha(patchA.CommitSha),
				patchB.Matching+1, truncateSha(patchB.CommitSha), patchB.Title,
			)
			diff := outputDiff(patchA, patchB)
			outputs = append(
				outputs,
				&RangeDiffOutput{
					Header: header,
					Diff:   diff,
					Type:   "diff",
				},
			)
		}
	}
	return outputs
}

func DoDiff(src, dst string) []diffmatchpatch.Diff {
	dmp := diffmatchpatch.New()
	wSrc, wDst, warray := dmp.DiffLinesToChars(src, dst)
	diffs := dmp.DiffMain(wSrc, wDst, false)
	diffs = dmp.DiffCharsToLines(diffs, warray)
	return diffs
}

func outputDiff(patchA, patchB *PatchRange) []diffmatchpatch.Diff {
	diffs := []diffmatchpatch.Diff{}
	for _, fileA := range patchA.Files {
		for _, fileB := range patchB.Files {
			if fileA.NewName == fileB.NewName {
				strA := "\n@@ " + fileA.NewName + "\n"
				for _, frag := range fileA.TextFragments {
					for _, line := range frag.Lines {
						strA += line.String()
					}
				}
				strB := "\n@@ " + fileB.NewName + "\n"
				for _, frag := range fileB.TextFragments {
					for _, line := range frag.Lines {
						strB += line.String()
					}
				}
				diffs = append(diffs, DoDiff(strA, strB)...)
			}
		}
	}

	return diffs
}

func outputPairHeader(a *PatchRange, b *PatchRange, aIndex, bIndex int) string {
	if a == nil {
		return fmt.Sprintf("-:  ------- > %d:  %s %s\n", bIndex, truncateSha(b.CommitSha), b.Title)
	}
	if b == nil {
		return fmt.Sprintf("%d:  %s < -:  ------- %s\n", aIndex, truncateSha(a.CommitSha), a.Title)
	}
	return fmt.Sprintf("%d:  %s = %d:  %s %s\n", aIndex, truncateSha(a.CommitSha), bIndex, truncateSha(b.CommitSha), a.Title)
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
		output += diff.Header
		for _, d := range diff.Diff {
			switch d.Type {
			case diffmatchpatch.DiffEqual:
				output += d.Text
			case diffmatchpatch.DiffInsert:
				output += d.Text
			case diffmatchpatch.DiffDelete:
				output += d.Text
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

// computeAssignment assigns patches using the Hungarian algorithm.
/* func computeAssignment(costMatrix [][]int, m, n int) []int {
	u := make([]int, m+1) // potential for workers
	v := make([]int, n+1) // potential for jobs
	p := make([]int, n+1) // job assignment
	way := make([]int, n+1)

	for i := 1; i <= m; i++ {
		links := make([]int, n+1)
		minV := make([]int, n+1)
		used := make([]bool, n+1)
		for j := 0; j <= n; j++ {
			minV[j] = math.MaxInt32
			used[j] = false
		}

		j0 := 0
		p[0] = i

		for {
			used[j0] = true
			i0 := p[j0]
			delta := math.MaxInt32
			j1 := 0

			for j := 1; j <= n; j++ {
				if !used[j] {
					cur := costMatrix[i0-1][j-1] - u[i0] - v[j]
					if cur < minV[j] {
						minV[j] = cur
						links[j] = j0
					}
					if minV[j] < delta {
						delta = minV[j]
						j1 = j
					}
				}
			}

			for j := 0; j <= n; j++ {
				if used[j] {
					u[p[j]] += delta
					v[j] -= delta
				} else {
					minV[j] -= delta
				}
			}

			j0 = j1
			if p[j0] == 0 {
				break
			}
		}

		for {
			j1 := way[j0]
			p[j0] = p[j1]
			j0 = j1
			if j0 == 0 {
				break
			}
		}
	}

	assignment := make([]int, m)
	for j := 1; j <= n; j++ {
		if p[j] > 0 {
			assignment[p[j]-1] = j - 1
		}
	}
	return assignment
} */
