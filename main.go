package main

import (
	"context"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/patrickmn/go-cache"
)

var (
	stdout         = flag.Bool("stdout", false, "Output root dir to stdout and quit")
	basePath       = flag.String("base_path", ".", "Base path")
	port           = flag.Int("port", 8099, "Listening port")
	cacheDuration  = flag.Duration("cache_duration", 30*time.Second, "Cache duration")
	apologyTimeout = flag.Duration("apology_timeout", 2*time.Second, "Time after which to show 'this is taking a while'")
)

var pathCache *cache.Cache

type reportEntry struct {
	name    string
	bytes   uint64
	ratio   float64
	subdirs report
}

type report []reportEntry

type plotlyData struct {
	IDs     []string `json:"ids"`
	Labels  []string `json:"labels"`
	Parents []string `json:"parents"`
	Values  []uint64 `json:"values"`
	Type    string   `json:"type"`
}

func (r report) toParentsAndValues(parent string, parents map[string]string, values map[string]uint64) {
	for _, re := range r {
		parents[re.name] = parent
		if re.subdirs == nil {
			values[re.name] = re.bytes
		} else {
			re.subdirs.toParentsAndValues(re.name, parents, values)
		}
	}
}

func (r report) toPlotlyData() plotlyData {
	var result = plotlyData{Type: "sunburst"}

	parents := map[string]string{}
	values := map[string]uint64{}

	r.toParentsAndValues("", parents, values)

	result.IDs = make([]string, 0, len(parents))
	for k := range parents {
		result.IDs = append(result.IDs, k)
	}
	sort.Strings(result.IDs)

	result.Labels = make([]string, len(parents))
	result.Parents = make([]string, len(parents))
	result.Values = make([]uint64, len(parents))

	for i, id := range result.IDs {
		result.Labels[i] = filepath.Base(id)
		result.Parents[i] = parents[id]
		result.Values[i] = values[id]
	}

	return result
}

func (r report) sum() uint64 {
	var result uint64
	for _, re := range r {
		result += uint64(re.bytes)
	}
	return result
}

func walk(basePath, subPath string) report {
	p := filepath.Join(basePath, subPath)
	if pathCache != nil {
		if result, found := pathCache.Get(p); found {
			return result.(report)
		}
	}

	dir, err := os.Open(p)
	if err != nil {
		log.Printf("Failed to open %s: %v", p, err)
		return nil
	}
	defer dir.Close()

	files, err := dir.Readdir(-1)
	if err != nil {
		log.Printf("Failed to readdir %s: %v", p, err)
		return nil
	}

	result := make(report, len(files))

	for i, file := range files {
		result[i].name = filepath.Join(subPath, file.Name())
		if file.IsDir() {
			result[i].subdirs = walk(basePath, path.Join(subPath, file.Name()))
			result[i].bytes = result[i].subdirs.sum()
		} else {
			result[i].bytes = uint64(file.Size())
		}
	}

	sort.Slice(result, func(i, j int) bool { return result[i].bytes > result[j].bytes })

	result.computeRatios()

	if pathCache != nil {
		pathCache.Set(p, result, cache.DefaultExpiration)
	}

	return result
}

func (r report) computeRatios() {
	sum := float64(r.sum())
	for i := range r {
		r[i].ratio = float64(r[i].bytes) / sum
	}
}

func fmtPercent(ratio float64) string {
	return fmt.Sprintf("%.1f%%", ratio*100)
}

func main() {
	flag.Parse()

	pathCache = cache.New(*cacheDuration, *cacheDuration)

	if *stdout {
		walk(*basePath, "").output(os.Stdout)
		return
	}

	http.Handle("/", handler{basePath: *basePath})
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", *port), nil))
}

var (
	headerTemplate = template.Must(template.ParseFiles("header.html"))
	tableTemplate  = template.Must(template.ParseFiles("table.html"))
)

type humanReport []struct {
	Name       string
	Percentage string
	Size       string
}

func (r report) humanize() humanReport {
	result := make(humanReport, len(r))

	for i := range r {
		result[i].Name = r[i].name
		result[i].Percentage = fmtPercent(r[i].ratio)
		result[i].Size = humanize.Bytes(r[i].bytes)
	}
	return result
}

type handler struct {
	basePath       string
	artificalDelay time.Duration
}

func (h handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var wm sync.Mutex
	requestedPath := r.FormValue("path")
	if requestedPath == "" {
		requestedPath = "/"
	}
	flusher := w.(http.Flusher)
	if flusher == nil {
		log.Println("Flusher not available for chunked encoding")
	}
	actualPath := path.Join(h.basePath, requestedPath)

	wm.Lock()
	err := headerTemplate.Execute(w, struct {
		Path string
	}{
		Path: requestedPath,
	})
	if err != nil {
		log.Printf("Internal error: %v", err)
		http.Error(w, "Internal server error (see log)", http.StatusInternalServerError)
	}

	if flusher != nil {
		flusher.Flush()
	}
	wm.Unlock()

	ctx, cancel := context.WithCancel(r.Context())

	var wg sync.WaitGroup
	defer wg.Wait()

	wg.Add(1)
	go func() {
		defer wg.Done()
		rep := walk(actualPath, "")
		time.Sleep(h.artificalDelay)
		var plotly plotlyData
		if r.FormValue("plotly") == "1" {
			plotly = rep.toPlotlyData()
		}
		cancel()

		wm.Lock()
		err = tableTemplate.Execute(w, struct {
			Path       string
			Parent     string
			Report     humanReport
			PlotlyData plotlyData
			Total      string
		}{
			Path:       requestedPath,
			Parent:     path.Dir(requestedPath),
			Report:     rep.humanize(),
			PlotlyData: plotly,
			Total:      humanize.Bytes(rep.sum()),
		})
		if err != nil {
			log.Printf("Internal error: %v", err)
			http.Error(w, "Internal server error (see log)", http.StatusInternalServerError)
		}
		wm.Unlock()
	}()

	select {
	case <-time.After(*apologyTimeout):
		wm.Lock()
		fmt.Fprintln(w, "<p>Please wait, caches are cold...</p>")
		if flusher != nil {
			flusher.Flush()
		}
		wm.Unlock()
	case <-ctx.Done():
	}
}

func (r report) output(w io.Writer) {
	for _, re := range r {
		fmt.Fprintln(w, re.name, fmtPercent(re.ratio), humanize.Bytes(re.bytes))
	}
}
