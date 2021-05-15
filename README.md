# d64

d64 is a Commodore 64 .d64 diskimage library written in Go.

## Install Library

`go get github.com/staD020/d64`

## Features

* Create a .d64
* Add PRG files to a new or existing .d64
* Custom sector interleave (guesses based on existing files)
* Disk Label and 5 char Disk ID
* Extract all PRGs from a .d64

## Bugs & Missing Features

* DEL/REL/SEQ/USR files are ignored, they will be overwritten
* You can add files with the same filename
* Scratch/delete
* 36+ tracks
* Per file sector interleave
* DirArt
* Optional file storage on the DirTrack
* Custom filenames

I'm actually not planning on handling these or other features, unless I need them myself.
There are other d64 tools around with far more capabilities, check [cc1541](https://bitbucket.org/PTV_Claus/cc1541) for example.

Pull requests are welcome though.

## Command-line Interface

`go install github.com/staD020/d64/cmd/d64`

## Build from source

`go test -v -cover -bench . -benchmem && go build -v ./cmd/d64`

## Examples

A couple of common examples, error-handling omitted.

```go
package main

import (
	"fmt"
	"github.com/staD020/d64"
)

func main() {
	exampleNewDisk()
	exampleLoadDisk()
	exampleExtract()
}

func exampleNewDisk() {
	d := d64.NewDisk("a new disk", "01 2a", d64.DefaultSectorInterleave)
	_ = d.AddFile("foo.prg", "foo")
	_ = d.AddFile("bar.prg", "bar")
	_ = d.WriteFile("foo.d64")

	fmt.Println("Directory:")
	fmt.Println(d)
}

func exampleLoadDisk() {
	d, _ := d64.LoadDisk("foo.d64")
	_ = d.AddFile("baz.prg", "baz")
	_ = d.WriteFile("foo.d64")

	fmt.Println("Directory:")
	fmt.Println(d)
}

func exampleExtract() {
	d, _ := d64.LoadDisk("foo.d64")
	_ = d.ExtractToPath(".")
}
```
