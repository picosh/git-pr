package git

import (
	"fmt"
	"math"

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

func output(a []*PatchRange, b []*PatchRange) string {
	out := ""
	for i, patchA := range a {
		if patchA.Matching == -1 {
			out += outputPairHeader(patchA, nil, i+1, -1)
		}
	}

	for j, patchB := range b {
		if patchB.Matching == -1 {
			out += outputPairHeader(nil, patchB, -1, j+1)
			continue
		}
		patchA := a[patchB.Matching]
		if patchB.ContentSha == patchA.ContentSha {
			out += outputPairHeader(patchA, patchB, patchB.Matching+1, patchA.Matching+1)
		}
	}
	return out
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

func RangeDiff(a []*Patch, b []*Patch) string {
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
	return len(dmp.DiffPrettyText(diffs))
}

func getCorrespondences(a []*PatchRange, b []*PatchRange, creationFactor int) {
	n := len(a) + len(b)
	fmt.Println("rows", len(a), "cols", len(b))
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

	assignment := computeAssignment(cost, n, n)
	for i, j := range assignment {
		if i < len(a) && j < len(b) {
			a[i].Matching = j
			b[j].Matching = i
		}
	}

	fmt.Println("cost", cost, "assignment", assignment)
}

// computeAssignment assigns patches using the Hungarian algorithm.
func computeAssignment(costMatrix [][]int, m, n int) []int {
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
}
