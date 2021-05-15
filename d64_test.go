package d64

import (
	"bytes"
	"io/ioutil"
	"os"
	"strconv"
	"testing"
)

const (
	defaultD64Size = 174848

	validatedD64 = "testdata/validated.d64"
	testPrg1     = "testdata/testfile1.prg"
	testPrg2     = "testdata/testfile2.prg"
	testLongPrg  = "testdata/enforcer.prg"

	testD64         = "testdata/lastnight.d64"
	testD64Label    = "8580 stinsen"
	testD64DiskID   = "01 2a"
	testD64NumFiles = 20
)

var testFileLength = []int{4421, 6921, 4752, 6675, 3918, 3471, 6251, 5710, 5578, 4011, 22529, 7325, 7794, 8964, 7768, 9452, 8948, 6306, 7339, 25089}
var testFileBlocks = []int{18, 28, 19, 27, 16, 14, 25, 23, 22, 16, 89, 29, 31, 36, 31, 38, 36, 25, 29, 99}

var testPrgs = []string{testPrg1, testPrg2}

func TestNewDisk(t *testing.T) {
	label, diskID := "testnewdisk", "votox"
	d := NewDisk(label, diskID, DefaultSectorInterleave)
	if d.Label != label {
		t.Errorf("disk label incorrect, got: %q want: %q", d.Label, label)
	}
	if d.DiskID != diskID {
		t.Errorf("disk diskID incorrect, got: %q want: %q", d.DiskID, diskID)
	}

	out, err := ioutil.TempFile("", "testnewdisk.*.d64")
	if err != nil {
		t.Fatalf("ioutil.TempFile error: %v", err)
	}
	defer os.Remove(out.Name())

	_, err = d.WriteTo(out)
	if err != nil {
		t.Fatalf("d.WriteTo %q error: %v", out.Name(), err)
	}
}

func TestLoadDisk(t *testing.T) {
	d, err := LoadDisk(testD64)
	if err != nil {
		t.Fatalf("LoadDisk %q error: %v", testD64, err)
	}
	if d.Label != testD64Label {
		t.Errorf("LoadDisk Label mismatch, got %s want %s", d.Label, testD64Label)
	}
	if d.DiskID != testD64DiskID {
		t.Errorf("LoadDisk DiskID mismatch, got %s want %s", d.DiskID, testD64DiskID)
	}
	if d.SectorInterleave != DefaultSectorInterleave {
		t.Errorf("LoadDisk SectorInterleave failed, got %d want %d", d.SectorInterleave, DefaultSectorInterleave)
	}
}

func TestDirectory(t *testing.T) {
	d, err := LoadDisk(testD64)
	if err != nil {
		t.Fatalf("LoadDisk %q error: %v", testD64, err)
	}

	dir := d.Directory()
	if len(dir) != testD64NumFiles {
		t.Errorf("Directory %q number of files got %d want %d", testD64, len(dir), testD64NumFiles)
	}
	for n, e := range dir {
		if e.BlockSize != testFileBlocks[n] {
			t.Errorf("Directory %q BlockSize mismatch, got %d want %d", testD64, e.BlockSize, testFileBlocks[n])
		}
	}
}

func TestDiskExtract(t *testing.T) {
	d, err := LoadDisk(testD64)
	if err != nil {
		t.Fatalf("LoadDisk %q error: %v", testD64, err)
	}
	for n, e := range d.Directory() {
		prg := d.Extract(e.Track, e.Sector)
		if prg[0] != 0x01 || prg[1] != 0x08 {
			t.Errorf("d.Extract(%d, %d) loadaddress for file %q is not $0801.", e.Track, e.Sector, e.Filename)
		}
		if len(prg) != testFileLength[n] {
			t.Errorf("d.Extract(%d, %d) length got %d want %d.", e.Track, e.Sector, len(prg), testFileLength[n])
		}
	}
}

func TestDiskWriteFile(t *testing.T) {
	d, err := LoadDisk(testD64)
	if err != nil {
		t.Fatalf("LoadDisk %q error: %v", testD64, err)
	}

	out, err := ioutil.TempFile("", "testdiskwriteto.*.d64")
	if err != nil {
		t.Fatalf("ioutil.TempFile error: %v", err)
	}
	if err = out.Close(); err != nil {
		t.Fatalf("out.Close %q error: %v", out.Name(), err)
	}
	defer os.Remove(out.Name())

	err = d.WriteFile(out.Name())
	if err != nil {
		t.Fatalf("d.WriteTo error: %v", err)
	}

	compareOk, err := compareFiles(testD64, out.Name())
	if err != nil {
		t.Fatalf("compareFiles(%q, %q) failed: %v", testD64, out.Name(), err)
	}
	if !compareOk {
		t.Errorf("compareFiles(%q, %q) mismatch: files are different", testD64, out.Name())
	}
}

func TestTrackSectorToDataOffset(t *testing.T) {
	cases := []struct {
		track, sector byte
		want          int
	}{
		{1, 0, 0},
		{1, 1, 0x100},
		{1, 20, 0x1400},
		{2, 0, 0x1500},
		{2, 1, 0x1600},
		{3, 0, 0x2a00},
		{3, 1, 0x2b00},
		{17, 0, 0x15000},
		{18, 0, 0x16500},
		{35, 10, 0x2a400},
	}
	for _, c := range cases {
		got := trackSectorToDataOffset(c.track, c.sector)
		if got != c.want {
			t.Errorf("trackSectorToDataOffset(%d, %d) == %d, want %d", c.track, c.sector, got, c.want)
		}
	}
}

func TestDiskValidate(t *testing.T) {
	out, err := ioutil.TempFile("", "testdiskvalidate.*.d64")
	if err != nil {
		t.Fatalf("ioutil.TempFile error: %v", err)
	}
	defer out.Close()
	defer os.Remove(out.Name())

	d, err := LoadDisk(testD64)
	if err != nil {
		t.Fatalf("LoadDisk %q error: %v", testD64, err)
	}

	d.FormatBAM()
	d.Validate()
	dirEntries := d.Directory()
	for i := range testFileBlocks {
		if dirEntries[i].BlockSize != testFileBlocks[i] {
			t.Errorf("blocksize incorrect for %q: got %d want %d", d.Label, dirEntries[i].BlockSize, testFileBlocks[i])
		}
	}

	if _, err = d.WriteTo(out); err != nil {
		t.Fatalf("d.WriteTo %q error: %v", out.Name(), err)
	}
}

func TestDiskAddFile(t *testing.T) {
	d := NewDisk("d.addfile", "votox", DefaultSectorInterleave)

	prgLengths := []int{}
	for n, filename := range testPrgs {
		prg, err := ioutil.ReadFile(filename)
		if err != nil {
			t.Fatalf("ioutil.ReadFile %q failed: %v", filename, err)
		}
		prgLengths = append(prgLengths, len(prg))

		if err = d.AddPrg("file "+strconv.Itoa(n), prg); err != nil {
			t.Fatalf("d.AddFile %q failed: %v", filename, err)
		}
	}

	got := bytes.NewBuffer(make([]byte, 0, defaultD64Size))
	if _, err := d.WriteTo(got); err != nil {
		t.Fatalf("d.WriteTo failed: %v", err)
	}
	want, err := ioutil.ReadFile(validatedD64)
	if err != nil {
		t.Fatalf("ioutil.ReadFile %q failed: %v", validatedD64, err)
	}
	if !bytes.Equal(got.Bytes(), want) {
		t.Errorf("created d64 does not match %q failed", validatedD64)
	}

	for n, e := range d.Directory() {
		prg := d.Extract(e.Track, e.Sector)
		if len(prg) != prgLengths[n] {
			t.Errorf("file %q (tr %d sec %d) length mismatch. got %d want %d", e.Filename, e.Track, e.Sector, len(prg), prgLengths[n])
		}
	}
}

func TestExtractToPath(t *testing.T) {
	out, err := ioutil.TempDir("", "testdiskextract")
	if err != nil {
		t.Fatalf("ioutil.TempDir error: %v", err)
	}
	defer os.RemoveAll(out)

	d, err := LoadDisk(testD64)
	if err != nil {
		t.Fatalf("LoadDisk %q error: %v", testD64, err)
	}

	if err = d.ExtractToPath(out); err != nil {
		t.Fatalf("d.ExtractToPath %q to %q error: %v", testD64, out, err)
	}
}

func TestLongPrg(t *testing.T) {
	const longInterleave = 8
	d := NewDisk("long prg", "long", longInterleave)
	if err := d.AddFile(testLongPrg, "enforcer+6hi/scs"); err != nil {
		t.Errorf("d.AddFile %q error: %v", testLongPrg, err)
	}
	for _, e := range d.Directory() {
		if e.BlockSize != 471 {
			t.Errorf("incorrect blocksize, got %d want %d", e.BlockSize, 471)
		}
		break
	}

	out, err := ioutil.TempFile("", "testlongprg.*.d64")
	if err != nil {
		t.Fatalf("ioutil.TempFile error: %v", err)
	}
	defer os.Remove(out.Name())
	if _, err = d.WriteTo(out); err != nil {
		t.Fatalf("d.WriteTo %q error: %v", out.Name(), err)
	}
	if err = out.Close(); err != nil {
		t.Fatalf("out.Close %q error: %v", out.Name(), err)
	}

	d2, err := LoadDisk(out.Name())
	if err != nil {
		t.Fatalf("LoadDisk %q error: %v", out.Name(), err)
	}
	if d2.SectorInterleave != longInterleave {
		t.Errorf("incorrect SectorInterleave, got %d want %d", d2.SectorInterleave, longInterleave)
	}
}

// compareFiles loads files a and b and compares its contents.
// returns true if equal.
func compareFiles(a, b string) (bool, error) {
	bufa, err := ioutil.ReadFile(a)
	if err != nil {
		return false, err
	}
	bufb, err := ioutil.ReadFile(b)
	if err != nil {
		return false, err
	}
	return bytes.Equal(bufa, bufb), nil
}

func TestNormalizeFilename(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"filename", "filename"},
		{"File.Name", "file.name"},
		{" filename ", "filename"},
		{"	filename	", "filename"},
		{"1234567890123456", "1234567890123456"},
		{"123^4567-8901#23,456", "1234567-89012345"},
		{"12345678901234567", "1234567890123456"},
		{"enforcer+6hi/[_]", "enforcer+6hi/[_]"},
	}
	for _, c := range cases {
		got := NormalizeFilename(c.in)
		if got != c.want {
			t.Errorf("NormalizeFilename(%q) == %q, want %q", c.in, got, c.want)
		}
	}
}

func BenchmarkNormalizeFilename(b *testing.B) {
	for i := 0; i < b.N; i++ {
		NormalizeFilename("testfilename")
	}
}

func TestSizeToBlocks(t *testing.T) {
	cases := []struct {
		in, want int
	}{
		{0, 1},
		{253, 1},
		{254, 2},
		{500, 2},
		{512, 3},
		{4096, 17},
		{119482, 471},
	}
	for _, c := range cases {
		got := SizeToBlocks(c.in)
		if got != c.want {
			t.Errorf("SizeToBlocks(%d) == %d, want %d", c.in, got, c.want)
		}
	}
}

func BenchmarkSizeToBlocks(b *testing.B) {
	for i := 0; i < b.N; i++ {
		SizeToBlocks(i)
	}
}
