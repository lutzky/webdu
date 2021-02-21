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
	"github.com/jonboulle/clockwork"
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

var wantD3Data = d3Data{
	Name: "/",
	Children: []d3Data{
		{Name: "a", Value: 2},
		{Name: "b", Value: 3},
		{Name: "c", Children: []d3Data{
			{Name: "c/d", Value: 4},
		}},
		{Name: "emptyDir"},
	},
}

func TestMain(m *testing.M) {
	flag.Parse()

	// Git does not store empty directories, so we must create it manually
	os.MkdirAll("testdata/base_path/emptyDir", 0750)

	os.Exit(m.Run())
}

func TestToD3Data(t *testing.T) {
	got := wantReport.toD3Data("/")

	if diff := cmp.Diff(wantD3Data, got,
		cmp.AllowUnexported(d3Data{}),
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
		goldenFile      string
		artificialDelay time.Duration
		waitForApology  bool
	}{
		{goldenFile: "testdata/want.html", artificialDelay: 500 * time.Millisecond, waitForApology: false},
		{goldenFile: "testdata/want_slow.html", artificialDelay: 5 * time.Second, waitForApology: true},
	}

	origClock := clock
	defer func() { clock = origClock }()

	for _, tc := range testCases {
		t.Run(tc.goldenFile, func(t *testing.T) {
			fakeClock := clockwork.NewFakeClock()
			clock = fakeClock
			ts := httptest.NewServer(handler{
				basePath:        "testdata/base_path",
				artificialDelay: tc.artificialDelay,
			})
			defer ts.Close()

			go func() {
				t.Logf("Blocking for both clock.After(*apologyTimeout=%s) and clock.Sleep(%s)", *apologyTimeout, tc.artificialDelay)
				fakeClock.BlockUntil(2)

				if tc.waitForApology {
					t.Logf("Advancing clock by apologyTimeout (%s)", *apologyTimeout)
					fakeClock.Advance(*apologyTimeout)
					t.Logf("clock.After(*apologyTimeout=%s) should trigger, clock.Sleep(%s) should still sleep", *apologyTimeout, tc.artificialDelay)
					t.Logf("Blocking for apology to be done writing (clock.Sleep(%s) still sleeping)", tc.artificialDelay)
					fakeClock.BlockUntil(2)
					t.Logf("Apology now done writing, advancing to release clock.Sleep(%s)", tc.artificialDelay)
					fakeClock.Advance(tc.artificialDelay)
				} else {
					t.Logf("Advancing clock by artificalDelay (%s)", tc.artificialDelay)
					fakeClock.Advance(tc.artificialDelay)
				}
			}()
			res, err := http.Get(ts.URL + "?d3=1")
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
