package git

import (
	"fmt"
	"strings"
	"testing"

	"github.com/picosh/git-pr/fixtures"
)

func bail(err error) {
	if err != nil {
		panic(bail)
	}
}

func cmp(afile, bfile string) string {
	a, err := fixtures.Fixtures.Open(afile)
	bail(err)
	b, err := fixtures.Fixtures.Open(bfile)
	bail(err)
	aPatches, err := ParsePatchset(a)
	bail(err)
	bPatches, err := ParsePatchset(b)
	bail(err)
	actual := RangeDiff(aPatches, bPatches)
	return RangeDiffToStr(actual)
}

func fail(expected, actual string) string {
	return fmt.Sprintf("expected:[\n%s] actual:[\n%s]", expected, actual)
}

// https://git.kernel.org/tree/t/t3206-range-diff.sh?id=d19b6cd2dd72dc811f19df4b32c7ed223256c3ee

// simple A..B A..C (unmodified)
/*
	1:  $(test_oid t1) = 1:  $(test_oid u1) s/5/A/
	2:  $(test_oid t2) = 2:  $(test_oid u2) s/4/A/
	3:  $(test_oid t3) = 3:  $(test_oid u3) s/11/B/
	4:  $(test_oid t4) = 4:  $(test_oid u4) s/12/B/
*/
func TestRangeDiffUnmodified(t *testing.T) {
	actual := cmp("a_b.patch", "a_c.patch")
	expected := "1:  33c682a = 1:  1668484 chore: add torch and create random tensor\n"
	if expected != actual {
		t.Fatal(fail(expected, actual))
	}
}

// trivial reordering
/*
	1:  $(test_oid t1) = 1:  $(test_oid r1) s/5/A/
	3:  $(test_oid t3) = 2:  $(test_oid r2) s/11/B/
	4:  $(test_oid t4) = 3:  $(test_oid r3) s/12/B/
	2:  $(test_oid t2) = 4:  $(test_oid r4) s/4/A/
*/
func TestRangeDiffTrivialReordering(t *testing.T) {
	actual := cmp("a_b_reorder.patch", "a_c_reorder.patch")
	expected := `2:  22dde12 = 1:  7dbb94c docs: readme
1:  33c682a = 2:  ad17587 chore: add torch and create random tensor
`
	if expected != actual {
		t.Fatal(fail(expected, actual))
	}
}

// removed commit
/*
	1:  $(test_oid t1) = 1:  $(test_oid d1) s/5/A/
	2:  $(test_oid t2) < -:  $(test_oid __) s/4/A/
	3:  $(test_oid t3) = 2:  $(test_oid d2) s/11/B/
	4:  $(test_oid t4) = 3:  $(test_oid d3) s/12/B/
*/
func TestRangeDiffRemovedCommit(t *testing.T) {
	actual := cmp("a_b_reorder.patch", "a_c_rm_commit.patch")
	if !strings.Contains(actual, "1:  33c682a < -:  ------- chore: add torch and create random tensor") {
		t.Fatal("expected removed commit header not found")
	}
	if !strings.Contains(actual, "2:  22dde12 = 1:  7dbb94c docs: readme") {
		t.Fatal("expected equal commit header not found")
	}
	if !strings.Contains(actual, "requirements.txt") {
		t.Fatal("expected file diff for removed commit")
	}
}

// added commit
/*
	1:  $(test_oid t1) = 1:  $(test_oid a1) s/5/A/
	2:  $(test_oid t2) = 2:  $(test_oid a2) s/4/A/
	-:  $(test_oid __) > 3:  $(test_oid a3) s/6/A/
	3:  $(test_oid t3) = 4:  $(test_oid a4) s/11/B/
	4:  $(test_oid t4) = 5:  $(test_oid a5) s/12/B/
*/
func TestRangeDiffAddedCommit(t *testing.T) {
	actual := cmp("a_b_reorder.patch", "a_c_added_commit.patch")
	if !strings.Contains(actual, "1:  33c682a = 1:  33c682a chore: add torch and create random tensor") {
		t.Fatal("expected first equal commit header not found")
	}
	if !strings.Contains(actual, "2:  22dde12 = 2:  22dde12 docs: readme") {
		t.Fatal("expected second equal commit header not found")
	}
	if !strings.Contains(actual, "-:  ------- > 3:  b248060 chore: make tensor 6x6") {
		t.Fatal("expected added commit header not found")
	}
	if !strings.Contains(actual, "train.py") {
		t.Fatal("expected file diff for added commit")
	}
}

// changed commit
/*
	1:  $(test_oid t1) = 1:  $(test_oid c1) s/5/A/
	2:  $(test_oid t2) = 2:  $(test_oid c2) s/4/A/
	3:  $(test_oid t3) ! 3:  $(test_oid c3) s/11/B/
	    @@ file: A
	      9
	      10
	     -11
	    -+B
	    ++BB
	      12
	      13
	      14
	4:  $(test_oid t4) ! 4:  $(test_oid c4) s/12/B/
	    @@ file
	     @@ file: A
	      9
	      10
	    - B
	    + BB
	     -12
	     +B
	      13
*/
func TestRangeDiffChangedCommit(t *testing.T) {
	actual := cmp("a_b_reorder.patch", "a_c_changed_commit.patch")
	// os.WriteFile("fixtures/expected_commit_changed.txt", []byte(actual), 0644)
	fp, err := fixtures.Fixtures.ReadFile("expected_commit_changed.txt")
	if err != nil {
		t.Fatal("file not found")
	}
	expected := string(fp)
	if strings.TrimSpace(expected) != strings.TrimSpace(actual) {
		t.Fatal(fail(expected, actual))
	}
}

// renamed file
/*
	1:  $(test_oid t1) = 1:  $(test_oid n1) s/5/A/
	2:  $(test_oid t2) ! 2:  $(test_oid n2) s/4/A/
	    @@ Metadata
	    ZAuthor: Thomas Rast <trast@inf.ethz.ch>
	    Z
	    Z ## Commit message ##
	    -    s/4/A/
	    +    s/4/A/ + rename file
	    Z
	    - ## file ##
	    + ## file => renamed-file ##
	    Z@@
	    Z 1
	    Z 2
	3:  $(test_oid t3) ! 3:  $(test_oid n3) s/11/B/
	    @@ Metadata
	    Z ## Commit message ##
	    Z    s/11/B/
	    Z
	    - ## file ##
	    -@@ file: A
	    + ## renamed-file ##
	    +@@ renamed-file: A
	    Z 8
	    Z 9
	    Z 10
	4:  $(test_oid t4) ! 4:  $(test_oid n4) s/12/B/
	    @@ Metadata
	    Z ## Commit message ##
	    Z    s/12/B/
	    Z
	    - ## file ##
	    -@@ file: A
	    + ## renamed-file ##
	    +@@ renamed-file: A
	    Z 9
	    Z 10
	    Z B
*/
func TestRangeDiffRenamedFile(t *testing.T) {
	actual := cmp("a_b_reorder.patch", "a_c_renamed_file.patch")
	if !strings.Contains(actual, "1:  33c682a = 1:  33c682a") {
		t.Fatal("expected first commit to be equal")
	}
	if !strings.Contains(actual, "2:  22dde12 ! 2:  aabbcc1") {
		t.Fatal("expected second commit to show diff marker")
	}
	if !strings.Contains(actual, "DOCS.md") {
		t.Fatal("expected renamed file DOCS.md in output")
	}
}

// file with mode only change
/*
	1:  $(test_oid t2) ! 1:  $(test_oid o1) s/4/A/
	    @@ Metadata
	    ZAuthor: Thomas Rast <trast@inf.ethz.ch>
	    Z
	    Z ## Commit message ##
	    -    s/4/A/
	    +    s/4/A/ + add other-file
	    Z
	    Z ## file ##
	    Z@@
	    @@ file
	    Z A
	    Z 6
	    Z 7
	    +
	    + ## other-file (new) ##
	2:  $(test_oid t3) ! 2:  $(test_oid o2) s/11/B/
	    @@ Metadata
	    ZAuthor: Thomas Rast <trast@inf.ethz.ch>
	    Z
	    Z ## Commit message ##
	    -    s/11/B/
	    +    s/11/B/ + mode change other-file
	    Z
	    Z ## file ##
	    Z@@ file: A
	    @@ file: A
	    Z 12
	    Z 13
	    Z 14
	    +
	    + ## other-file (mode change 100644 => 100755) ##
	3:  $(test_oid t4) = 3:  $(test_oid o3) s/12/B/
*/
func TestRangeDiffFileWithModeOnlyChange(t *testing.T) {
	actual := cmp("a_b_reorder.patch", "a_c_mode_change.patch")
	if !strings.Contains(actual, "1:  33c682a ! 1:  33c682a") {
		t.Fatal("expected first commit to show diff marker due to new file")
	}
	if !strings.Contains(actual, "run.sh") {
		t.Fatal("expected run.sh script in output")
	}
}

// file added and later removed
/*
	1:  $(test_oid t1) = 1:  $(test_oid s1) s/5/A/
	2:  $(test_oid t2) ! 2:  $(test_oid s2) s/4/A/
	    @@ Metadata
	    ZAuthor: Thomas Rast <trast@inf.ethz.ch>
	    Z
	    Z ## Commit message ##
	    -    s/4/A/
	    +    s/4/A/ + new-file
	    Z
	    Z ## file ##
	    Z@@
	    @@ file
	    Z A
	    Z 6
	    Z 7
	    +
	    + ## new-file (new) ##
	3:  $(test_oid t3) ! 3:  $(test_oid s3) s/11/B/
	    @@ Metadata
	    ZAuthor: Thomas Rast <trast@inf.ethz.ch>
	    Z
	    Z ## Commit message ##
	    -    s/11/B/
	    +    s/11/B/ + remove file
	    Z
	    Z ## file ##
	    Z@@ file: A
	    @@ file: A
	    Z 12
	    Z 13
	    Z 14
	    +
	    + ## new-file (deleted) ##
	4:  $(test_oid t4) = 4:  $(test_oid s4) s/12/B/
*/
func TestRangeDiffFileAddedThenRemoved(t *testing.T) {
	actual := cmp("a_b_reorder.patch", "a_c_file_added_removed.patch")
	if !strings.Contains(actual, "1:  33c682a = 1:  33c682a") {
		t.Fatal("expected first commit to be equal")
	}
	if !strings.Contains(actual, "temp.txt") {
		t.Fatal("expected temp.txt in output")
	}
	if !strings.Contains(actual, "-:  ------- > 3:  ccddee1") {
		t.Fatal("expected third commit to be added")
	}
}

// changed message
/*
	1:  $(test_oid t1) = 1:  $(test_oid m1) s/5/A/
	2:  $(test_oid t2) ! 2:  $(test_oid m2) s/4/A/
	    @@ Metadata
	    Z ## Commit message ##
	    Z    s/4/A/
	    Z
	    +    Also a silly comment here!
	    +
	    Z ## file ##
	    Z@@
	    Z 1
	3:  $(test_oid t3) = 3:  $(test_oid m3) s/11/B/
	4:  $(test_oid t4) = 4:  $(test_oid m4) s/12/B/
*/
func TestRangeDiffChangedMessage(t *testing.T) {
	actual := cmp("a_b_reorder.patch", "a_c_changed_message.patch")
	if !strings.Contains(actual, "1:  33c682a = 1:  33c682a") {
		t.Fatal("expected first commit to be equal")
	}
	if !strings.Contains(actual, "2:  22dde12 ! 2:  ddeeff1") {
		t.Fatal("expected second commit to show diff marker due to message change")
	}
}

func TestRangeDiffEmptyPatchset(t *testing.T) {
	a, err := fixtures.Fixtures.Open("a_b_reorder.patch")
	bail(err)
	aPatches, err := ParsePatchset(a)
	bail(err)
	bPatches := []*Patch{}

	actual := RangeDiff(aPatches, bPatches)
	result := RangeDiffToStr(actual)

	if len(aPatches) != 2 {
		t.Fatalf("expected 2 patches in a, got %d", len(aPatches))
	}
	if !strings.Contains(result, "< -:") {
		t.Fatal("expected removed commit markers when comparing to empty patchset")
	}
	if !strings.Contains(result, "1:  33c682a < -:") {
		t.Fatal("expected first commit to show as removed")
	}
	if !strings.Contains(result, "2:  22dde12 < -:") {
		t.Fatal("expected second commit to show as removed")
	}
}

func TestRangeDiffEmptyToNonEmpty(t *testing.T) {
	b, err := fixtures.Fixtures.Open("a_b_reorder.patch")
	bail(err)
	bPatches, err := ParsePatchset(b)
	bail(err)
	aPatches := []*Patch{}

	actual := RangeDiff(aPatches, bPatches)
	result := RangeDiffToStr(actual)

	if len(bPatches) != 2 {
		t.Fatalf("expected 2 patches in b, got %d", len(bPatches))
	}
	if !strings.Contains(result, "> 1:") {
		t.Fatal("expected added commit markers when comparing from empty patchset")
	}
	if !strings.Contains(result, "-:  ------- > 1:  33c682a") {
		t.Fatal("expected first commit to show as added")
	}
}

func TestRangeDiffSquashedCommits(t *testing.T) {
	actual := cmp("a_b_reorder.patch", "a_c_squashed.patch")

	if !strings.Contains(actual, "< -:") {
		t.Fatal("expected at least one commit to show as removed (squashed away)")
	}
	if !strings.Contains(actual, "aabbccd") {
		t.Fatal("expected squashed commit sha in output")
	}
}

func TestRangeDiffSplitCommits(t *testing.T) {
	actual := cmp("a_b_reorder.patch", "a_c_split.patch")

	if !strings.Contains(actual, "-:  ------- >") {
		t.Fatal("expected added commit marker for split commit")
	}
	if !strings.Contains(actual, "aabb112") {
		t.Fatal("expected new split commit sha (aabb112) in output")
	}
	if !strings.Contains(actual, "33c682a") {
		t.Fatal("expected original commit sha in output")
	}
}

func TestRangeDiffDifferentAuthor(t *testing.T) {
	actual := cmp("a_b_reorder.patch", "a_c_different_author.patch")

	if !strings.Contains(actual, "!") {
		t.Fatal("expected diff marker (!) for commits with different author")
	}
	if !strings.Contains(actual, "33c682a") && !strings.Contains(actual, "22dde12") {
		t.Fatal("expected commit shas in output")
	}
}

func TestRangeDiffMultipleFilesInCommit(t *testing.T) {
	actual := cmp("a_b_reorder.patch", "a_c_multi_file_change.patch")

	if !strings.Contains(actual, "1:  33c682a = 1:  33c682a") {
		t.Fatal("expected first commit to be equal")
	}
	if !strings.Contains(actual, "2:  22dde12 ! 2:  bbccdd1") {
		t.Fatal("expected second commit to show diff marker")
	}
	if !strings.Contains(actual, "CONTRIBUTING.md") {
		t.Fatal("expected CONTRIBUTING.md in diff output")
	}
	if !strings.Contains(actual, "LICENSE.md") {
		t.Fatal("expected LICENSE.md in diff output")
	}
}

func TestRangeDiffIgnoresContextLines(t *testing.T) {
	actual := cmp("context_lines_v1.patch", "context_lines_v2.patch")

	if !strings.Contains(actual, "=") {
		t.Fatal("expected equal marker (=) since +/- lines are identical")
	}
	if strings.Contains(actual, "!") {
		t.Fatal("should not show diff marker (!) when only context lines differ")
	}
	if strings.Contains(actual, "old_value") || strings.Contains(actual, "new_value") {
		t.Fatal("should not have file diff output when changes are equal")
	}
}

func TestRangeDiffNormalizesHunkHeaders(t *testing.T) {
	actual := cmp("hunk_header_v1.patch", "hunk_header_v2.patch")

	if !strings.Contains(actual, "=") {
		t.Fatal("expected equal marker (=) since changes are identical despite different hunk headers")
	}
	if strings.Contains(actual, "!") {
		t.Fatal("should not show diff marker (!) when only hunk header line numbers differ")
	}
	if strings.Contains(actual, "@@ server.go") {
		t.Fatal("should not have file diff output when changes are equal")
	}
}
