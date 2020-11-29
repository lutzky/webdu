package main

import (
	"flag"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

var update = flag.Bool("update", false, "Update golden test files")

var wantReport = report{
	{"a", 2, float64(2) / 9},
	{"b", 3, float64(3) / 9},
	{"c", 4, float64(4) / 9},
}

func TestWalk(t *testing.T) {
	got := walk("testdata/base_path")

	if diff := cmp.Diff(wantReport, got,
		cmp.AllowUnexported(reportEntry{}),
		cmpopts.SortSlices(func(x, y reportEntry) bool { return x.name < y.name }),
	); diff != "" {
		t.Errorf("diff -want +got:\n%s", diff)
	}
}

func TestHTTP(t *testing.T) {
	testCases := []struct {
		goldenFile     string
		artificalDelay time.Duration
	}{
		{"testdata/want.html", 0},
		{"testdata/want_slow.html", 3 * time.Second},
	}

	for _, tc := range testCases {
		t.Run(tc.goldenFile, func(t *testing.T) {
			ts := httptest.NewServer(handler{
				basePath:       "testdata/base_path",
				artificalDelay: tc.artificalDelay,
			})
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
				ioutil.WriteFile(tc.goldenFile, got, 0644)
			}

			want, err := ioutil.ReadFile(tc.goldenFile)
			if err != nil {
				t.Fatalf("Couldn't read golden file: %v", err)
			}

			if string(want) != string(got) {
				t.Errorf("%s doesn't match; run with -update and use git diff", tc.goldenFile)
			}
		})
	}
}

func Example_output() {
	wantReport.output(os.Stdout)

	// Output:
	// a 22.2% 2 B
	// b 33.3% 3 B
	// c 44.4% 4 B
}
