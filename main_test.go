package main

import (
	"flag"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-test/deep"
)

var update = flag.Bool("update", false, "Update golden test files")

var wantReport = report{
	{"a", 2, float64(2) / 9},
	{"b", 3, float64(3) / 9},
	{"c", 4, float64(4) / 9},
}

func TestWalk(t *testing.T) {
	got := walk("testdata/base_path")

	if diff := deep.Equal(got, wantReport); diff != nil {
		t.Error(strings.Join(diff, "\n"))
	}
}

func TestHTTP(t *testing.T) {
	const goldenFile = "testdata/want.html"

	ts := httptest.NewServer(handler{"testdata/base_path"})
	defer ts.Close()

	res, err := http.Get(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ioutil.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}

	if *update {
		ioutil.WriteFile(goldenFile, got, 0644)
	}

	want, err := ioutil.ReadFile(goldenFile)
	if err != nil {
		t.Fatalf("Couldn't read golden file: %v", err)
	}

	if string(want) != string(got) {
		t.Errorf("%s doesn't match; run with -update and use git diff", goldenFile)
	}
}

func Example_output() {
	wantReport.output(os.Stdout)

	// Output:
	// a 22.2% 2 B
	// b 33.3% 3 B
	// c 44.4% 4 B
}
