package main

import (
	"flag"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/staD020/d64"
)

const Version = "0.2"

var (
	flagAdd       string
	flagDirectory string
	flagExtract   string
	flagHelp      bool
	flagQuiet     bool
	flagVerbose   bool
)

func init() {
	flag.StringVar(&flagAdd, "add", "", "add files to .d64 (-add d64.d64 file1.prg file2.prg)")
	flag.StringVar(&flagAdd, "a", "", "add")
	flag.StringVar(&flagExtract, "extract", "", "extract .prgs from .d64 (-extract d64.d64)")
	flag.StringVar(&flagExtract, "e", "", "extract")
	flag.StringVar(&flagDirectory, "dir", "", "prints the directory from .d64 (-dir d64.d64)")
	flag.StringVar(&flagDirectory, "d", "", "dir")

	flag.BoolVar(&flagHelp, "help", false, "")
	flag.BoolVar(&flagHelp, "h", false, "help")
	flag.BoolVar(&flagQuiet, "quiet", false, "only output errors")
	flag.BoolVar(&flagQuiet, "q", false, "quiet")
	flag.BoolVar(&flagVerbose, "verbose", false, "")
	flag.BoolVar(&flagVerbose, "v", false, "verbose")
}

func main() {
	t0 := time.Now()
	flag.Parse()
	files := flag.Args()
	if !flagQuiet {
		fmt.Printf("d64 %s by burg\n", Version)
	}

	showUsage := true
	if flagAdd != "" {
		showUsage = false
		if err := addToD64(flagAdd, files); err != nil {
			if err = newD64(flagAdd, files); err != nil {
				panic(err)
			}
		}
		if !flagQuiet {
			fmt.Printf("added %d files to %q\n", len(files), flagAdd)
		}
	}

	if flagExtract != "" {
		showUsage = false
		if err := extractD64(flagExtract); err != nil {
			panic(err)
		}
		if !flagQuiet {
			fmt.Printf("extracted prg files from %q\n", flagExtract)
		}
	}

	if flagDirectory != "" {
		showUsage = false
		d, err := d64.LoadDisk(flagDirectory)
		if err != nil {
			panic(err)
		}
		fmt.Println(d)
	}

	if showUsage || flagHelp {
		fmt.Println("Usage: ./d64 [-v -q -h -a foo.d64 -d foo.d64 -e foo.d64] [FILE [FILES]]")
		fmt.Println()
		flag.PrintDefaults()
	}

	if !flagQuiet {
		fmt.Println("running time:", time.Now().Sub(t0))
	}
}

func newD64(path string, prgs []string) error {
	d := d64.NewDisk(filepath.Base(path), "01 2a", d64.DefaultSectorInterleave)
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

	if flagVerbose {
		fmt.Println(d)
	}
	return nil
}

func addToD64(path string, prgs []string) error {
	d, err := d64.LoadDisk(path)
	if err != nil {
		return fmt.Errorf("d64.LoadDisk %q failed: %v", path, err)
	}

	for _, prg := range prgs {
		name, ext := d64.NormalizeFilename(filepath.Base(prg)), filepath.Ext(prg)
		if strings.ToLower(ext) == ".prg" {
			name = strings.TrimSuffix(name, ext)
		}
		if err := d.AddFile(prg, name); err != nil {
			return fmt.Errorf("d.AddFile %q failed: %v", prg, err)
		}
	}

	if flagVerbose {
		fmt.Println(d)
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

	if flagVerbose {
		fmt.Println(d)
	}

	if _, err = d.ExtractToPath("."); err != nil {
		return fmt.Errorf("d.ExtractToPath %q failed: %v", ".", err)
	}
	return nil
}
