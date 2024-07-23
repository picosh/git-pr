package git

import (
	"fmt"
	"io"
	"os"
	"testing"
)

func TestParsePatchsetWithCover(t *testing.T) {
	file, err := os.Open("fixtures/with-cover.patch")
	defer func() {
		_ = file.Close()
	}()
	if err != nil {
		t.Fatal(err.Error())
	}
	actual, err := ParsePatchset(file)
	if err != nil {
		t.Fatal(err.Error())
	}
	expected := []*Patch{
		{Title: "Add torch deps"},
		{Title: "feat: lets build an rnn"},
		{Title: "chore: add torch to requirements"},
	}
	if len(actual) != len(expected) {
		t.Fatalf("patches not same length (expected:%d, actual:%d)\n", len(expected), len(actual))
	}
	for idx, act := range actual {
		exp := expected[idx]
		if exp.Title != act.Title {
			t.Fatalf("title does not match expected (expected:%s, actual:%s)", exp.Title, act.Title)
		}
	}
}

func TestPatchToDiff(t *testing.T) {
	file, err := os.Open("fixtures/single.patch")
	defer func() {
		_ = file.Close()
	}()
	if err != nil {
		t.Fatal(err.Error())
	}

	fileExp, err := os.Open("fixtures/single.diff")
	defer func() {
		_ = file.Close()
	}()
	if err != nil {
		t.Fatal(err.Error())
	}

	actual, err := patchToDiff(file)
	if err != nil {
		t.Fatal(err.Error())
	}

	by, err := io.ReadAll(fileExp)
	if err != nil {
		t.Fatal("cannot read expected file")
	}

	if actual != string(by) {
		fmt.Println(actual)
		t.Fatal("diff does not match expected")
	}
}
