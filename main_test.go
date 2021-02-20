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
	{
		name:  "a",
		bytes: 2,
		ratio: float64(2) / 9,
	},
	{
		name:  "b",
		bytes: 3,
		ratio: float64(3) / 9,
	},
	{
		name:  "c",
		bytes: 4,
		ratio: float64(4) / 9,
		isDir: true,
		subdirs: report{
			{
				name:  "c/d",
				bytes: 4,
				ratio: 1,
			},
		},
	},
	{
		name:    "emptyDir",
		bytes:   0,
		ratio:   0,
		isDir:   true,
		subdirs: report{},
	},
}

var wantPlotlyData = plotlyData{
	IDs:     []string{"a", "b", "c", "c/d", "emptyDir"},
	Labels:  []string{"a", "b", "c", "d", "emptyDir"},
	Parents: []string{"", "", "", "c", ""},
	Values:  []uint64{2, 3, 0, 4, 0},
	Type:    "sunburst",
}

func TestMain(m *testing.M) {
	flag.Parse()

	// Git does not store empty directories, so we must create it manually
	os.MkdirAll("testdata/base_path/emptyDir", 0750)

	os.Exit(m.Run())
}

func TestToPlotlyReport(t *testing.T) {
	got := wantReport.toPlotlyData()

	if diff := cmp.Diff(wantPlotlyData, got,
		cmp.AllowUnexported(plotlyData{}),
	); diff != "" {
		t.Errorf("diff -want +got:\n%s", diff)
	}
}

func TestWalk(t *testing.T) {
	got := walk("testdata/base_path", "")

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

			res, err := http.Get(ts.URL + "?plotly=1")
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
	// emptyDir 0.0% 0 B
}
