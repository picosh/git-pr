package git

import (
	"fmt"
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
	return actual
}

func fail(expected, actual string) string {
	return fmt.Sprintf("expected:[%s] actual:[%s]", expected, actual)
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
		t.Fatalf(fail(expected, actual))
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
		t.Fatalf(fail(expected, actual))
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
	expected := `1:  33c682a < -:  ------- chore: add torch and create random tensor
2:  22dde12 = 1:  7dbb94c docs: readme
`
	if expected != actual {
		t.Fatalf(fail(expected, actual))
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
	expected := `1:  33c682a = 1:  33c682a chore: add torch and create random tensor
2:  22dde12 = 2:  22dde12 docs: readme
-:  ------- > 3:  b248060 chore: make tensor 6x6
`
	if expected != actual {
		t.Fatalf(fail(expected, actual))
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
	fp, err := fixtures.Fixtures.ReadFile("expected_commit_changed.txt")
	if err != nil {
		t.Fatalf("file not found")
	}
	expected := string(fp)
	if expected != actual {
		t.Fatalf(fail(expected, actual))
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
// func TestRangeDiffRenamedFile(t *testing.T) {}

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
// func TestRangeDiffFileWithModeOnlyChange(t *testing.T) {}

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
// func TestRangeDiffFileAddedThenRemoved(t *testing.T) {}

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
// func TestRangeDiffChangedMessage(t *testing.T) {}
