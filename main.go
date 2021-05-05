package main

// test

import (
	"context"
	"embed"
	"encoding/json"
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
	"github.com/jonboulle/clockwork"
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

var clock = clockwork.NewRealClock()

type reportEntry struct {
	name    string
	bytes   uint64
	ratio   float64
	subdirs report
	isDir   bool
}

type report []reportEntry

type d3Data struct {
	Name     string   `json:"name"`
	Value    uint64   `json:"value,omitempty"`
	Children []d3Data `json:"children,omitempty"`
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

func (r report) toD3Data(name string) d3Data {
	var result = d3Data{Name: name}
	for _, child := range r {
		var entry d3Data
		if len(child.subdirs) == 0 {
			entry.Name = child.name
			entry.Value = child.bytes
		} else {
			entry = child.subdirs.toD3Data(child.name)
		}
		result.Children = append(result.Children, entry)
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
		result[i].isDir = file.IsDir()
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

	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFiles))))
	http.Handle("/", handler{basePath: *basePath})
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", *port), nil))
}

//go:embed header.html table.html
var templateFiles embed.FS
var templates = template.Must(template.ParseFS(templateFiles, "*.html"))

//go:embed icicle.js
var staticFiles embed.FS

type humanReport []struct {
	Name       string
	Percentage string
	Size       string
	IsDir      bool
}

func (r report) humanize() humanReport {
	result := make(humanReport, len(r))

	for i := range r {
		result[i].Name = filepath.Base(r[i].name)
		result[i].Percentage = fmtPercent(r[i].ratio)
		result[i].Size = humanize.Bytes(r[i].bytes)
		result[i].IsDir = r[i].isDir
	}
	return result
}

type handler struct {
	basePath string

	// For testing
	artificialDelay time.Duration
}

func (h handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var wm sync.Mutex
	requestedPath := r.FormValue("path")
	if requestedPath == "" {
		requestedPath = "/"
	}

	actualPath := path.Join(h.basePath, requestedPath)
	var loadD3 = false

	if r.FormValue("json") != "" {
		w.Header().Add("Content-Type", "text/json")
		rep := walk(actualPath, "").toD3Data(requestedPath)
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			log.Printf("Internal error: %v", err)
			http.Error(w, "Internal server error (see log)", http.StatusInternalServerError)
		}
		return
	}
	if r.FormValue("d3") != "" {
		loadD3 = true
	}

	flusher := w.(http.Flusher)
	if flusher == nil {
		log.Println("Flusher not available for chunked encoding")
	}

	wm.Lock()
	err := templates.ExecuteTemplate(w, "header.html", struct {
		Path   string
		LoadD3 bool
	}{
		Path:   requestedPath,
		LoadD3: loadD3,
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
		if h.artificialDelay > 0 {
			clock.Sleep(h.artificialDelay)
		}
		var d3 d3Data
		if loadD3 {
			d3 = rep.toD3Data(requestedPath)
		}
		cancel()

		wm.Lock()
		err = templates.ExecuteTemplate(w, "table.html", struct {
			Path   string
			Parent string
			Report humanReport
			D3Data d3Data
			Total  string
		}{
			Path:   requestedPath,
			Parent: path.Dir(requestedPath),
			Report: rep.humanize(),
			D3Data: d3,
			Total:  humanize.Bytes(rep.sum()),
		})
		if err != nil {
			log.Printf("Internal error: %v", err)
			http.Error(w, "Internal server error (see log)", http.StatusInternalServerError)
		}
		wm.Unlock()
	}()

	select {
	case <-clock.After(*apologyTimeout):
		wm.Lock()
		fmt.Fprintln(w, "<p>Please wait, caches are cold...</p>")
		if flusher != nil {
			flusher.Flush()
		}
		wm.Unlock()
		if h.artificialDelay > 0 {
			// Allow fakeClock to block until we're done writing apology
			clock.Sleep(1 * time.Second)
		}
	case <-ctx.Done():
	}
}

func (r report) output(w io.Writer) {
	for _, re := range r {
		fmt.Fprintln(w, re.name, fmtPercent(re.ratio), humanize.Bytes(re.bytes))
	}
}
