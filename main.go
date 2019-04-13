package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path"
	"sort"

	humanize "github.com/dustin/go-humanize"
)

var (
	stdout   = flag.Bool("stdout", true, "Output root dir to stdout and quit")
	basePath = flag.String("base_path", ".", "Base path")
)

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

func walk(basePath string) report {
	dir, err := os.Open(basePath)
	if err != nil {
		log.Printf("Failed to open %s: %v", basePath, err)
		return nil
	}
	defer dir.Close()

	files, err := dir.Readdir(-1)
	if err != nil {
		log.Printf("Failed to readdir %s: %v", basePath, err)
		return nil
	}

	result := make(report, len(files))

	for i, file := range files {
		result[i].name = file.Name()
		if file.IsDir() {
			result[i].bytes = walk(path.Join(basePath, file.Name())).sum()
		} else {
			result[i].bytes = uint64(file.Size())
		}
	}

	sort.Slice(result, func(i, j int) bool { return result[i].bytes < result[j].bytes })

	result.computeRatios()

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

	if *stdout {
		r := walk(*basePath)
		for _, re := range r {
			fmt.Println(re.name, fmtPercent(re.ratio), humanize.Bytes(re.bytes))
		}
	}
}
