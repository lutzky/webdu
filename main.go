package main

import (
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"sort"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/patrickmn/go-cache"
)

var (
	stdout        = flag.Bool("stdout", false, "Output root dir to stdout and quit")
	basePath      = flag.String("base_path", ".", "Base path")
	port          = flag.Int("port", 8099, "Listening port")
	cacheDuration = flag.Duration("cache_duration", 30*time.Second, "Cache duration")
)

var pathCache *cache.Cache

type reportEntry struct {
	name  string
	bytes uint64
	ratio float64
}

type report []reportEntry

func (r report) sum() uint64 {
	var result uint64
	for _, re := range r {
		result += uint64(re.bytes)
	}
	return result
}

func walk(p string) report {
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
		result[i].name = file.Name()
		if file.IsDir() {
			result[i].bytes = walk(path.Join(p, file.Name())).sum()
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
		walk(*basePath).output(os.Stdout)
		return
	}

	http.Handle("/", handler{basePath: *basePath})
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", *port), nil))
}

var tmpl = template.Must(template.ParseFiles("template.html"))

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
	basePath string
}

func (h handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	requestedPath := r.FormValue("path")
	if requestedPath == "" {
		requestedPath = "/"
	}
	actualPath := path.Join(h.basePath, requestedPath)
	rep := walk(actualPath)

	err := tmpl.Execute(w, struct {
		Parent string
		Path   string
		Report humanReport
		Total  string
	}{
		Path:   requestedPath,
		Parent: path.Dir(requestedPath),
		Report: rep.humanize(),
		Total:  humanize.Bytes(rep.sum()),
	})
	if err != nil {
		log.Printf("Internal error: %v", err)
		http.Error(w, "Internal server error (see log)", http.StatusInternalServerError)
	}
}

func (r report) output(w io.Writer) {
	for _, re := range r {
		fmt.Fprintln(w, re.name, fmtPercent(re.ratio), humanize.Bytes(re.bytes))
	}
}
