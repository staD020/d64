package main

import (
	"flag"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/staD020/d64"
)

const Version = "0.1"

var (
	flagNew     string
	flagExtract string
	flagQuiet   bool
	flagVerbose bool
)

func init() {
	flag.StringVar(&flagNew, "new", "", "create new .d64 (-new d64.d64)")
	flag.StringVar(&flagNew, "n", "", "new")

	flag.StringVar(&flagExtract, "extract", "", "extract .prgs from .d64 (-extract d64.d64)")
	flag.StringVar(&flagExtract, "e", "", "extract")

	flag.BoolVar(&flagQuiet, "quiet", false, "")
	flag.BoolVar(&flagQuiet, "q", false, "quiet")
	flag.BoolVar(&flagVerbose, "verbose", false, "")
	flag.BoolVar(&flagVerbose, "v", false, "verbose")
}

func main() {
	t0 := time.Now()
	flag.Parse()
	files := flag.Args()
	if !flagQuiet {
		fmt.Printf("d64 %s by burglar\n", Version)
	}

	var err error
	if flagNew != "" {
		err = newD64(flagNew, files)
		if err != nil {
			panic(err)
		}
		if !flagQuiet {
			fmt.Printf("created %q with %d files\n", flagNew, len(files))
		}
	}
	if flagExtract != "" {
		err = extractD64(flagExtract)
		if err != nil {
			panic(err)
		}
		if !flagQuiet {
			fmt.Printf("extracted prg files from %q\n", flagNew)
		}
	}

	if !flagQuiet {
		fmt.Println("running time:", time.Now().Sub(t0))
	}
}

func newD64(path string, prgs []string) error {
	d := d64.NewDisk(filepath.Base(path), d64.DefaultSectorInterleave)
	for _, prg := range prgs {
		name, ext := d64.NormalizeFilename(filepath.Base(prg)), filepath.Ext(prg)
		if strings.ToLower(ext) == ".prg" {
			name = strings.TrimSuffix(name, ext)
		}
		if err := d.AddFile(prg, name); err != nil {
			return fmt.Errorf("d.AddFile %q failed: %v", prg, err)
		}
	}
	if err := d.WriteFile(path); err != nil {
		return fmt.Errorf("d.WriteFile %q failed: %v", path, err)
	}
	return nil
}

func extractD64(path string) error {
	d, err := d64.LoadDisk(path)
	if err != nil {
		return fmt.Errorf("d64.LoadDisk %q failed: %v", path, err)
	}
	if err = d.ExtractToPath("."); err != nil {
		return fmt.Errorf("d.ExtractToPath %q failed: %v", ".", err)
	}
	return nil
}
