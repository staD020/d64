// Package d64 contains a pure-go implementation of the Commodore 64's .d64 disk image format.
package d64

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Definitions of .d64 requirements.
const (
	BlockSize       = 254 // Usable bytes per sector (block)
	MaxBlocks       = 664 // Max blocks per .d64 image
	MaxFilenameSize = 16  // Max filename size

	MaxTracks               = 35
	DirTrack                = 18
	DefaultSectorInterleave = 10
	DirInterleave           = 3
	MaxSectorsForBam        = 24
)

// A Sector represents a single sector.
type Sector struct {
	ID   byte
	Data [256]byte
}

// A Track represents a single track, consisting of multiple Sectors.
type Track struct {
	ID      byte
	Sectors []Sector
}

// A Disk represents a .d64 image.
type Disk struct {
	Name             string
	Tracks           []Track
	SectorInterleave byte
	Files            []string
	bam              [MaxTracks][MaxSectorsForBam]bool
	loadDisk         []byte
}

// A DirEntry represents a single file in the directory of this d64.
type DirEntry struct {
	Track     byte
	Sector    byte
	Filename  string
	BlockSize int
}

// TrackLink returns this sectors next track-link.
func (s *Sector) TrackLink() byte {
	return s.Data[0]
}

// SetTrackLink sets the track-link for the next sector of this file.
func (s *Sector) SetTrackLink(b byte) {
	s.Data[0] = b
}

// SectorLink returns this sectors next sector-link.
func (s *Sector) SectorLink() byte {
	return s.Data[1]
}

// SetSectorLink sets the sector-link for the next sector of this file.
func (s *Sector) SetSectorLink(b byte) {
	s.Data[1] = b
}

// Content returns the binary content of this sector, track&sector link are not included.
func (s *Sector) Content() []byte {
	if s.TrackLink() == 0 {
		return s.Data[2 : s.SectorLink()+1]
	}
	return s.Data[2:]
}

var sectorsPerTrack = [...]byte{
	0, // Track 0 is not supported

	21, 21, 21, 21, 21, 21, 21, 21, 21, 21, 21, 21, 21, 21, 21, 21, 21, // 1-17
	19, 19, 19, 19, 19, 19, 19, // 18-24
	18, 18, 18, 18, 18, 18, // 25-30
	17, 17, 17, 17, 17, // 31-35
	17, 17, 17, 17, 17, // 36-40 (not tested)
}

func totalSectors(track byte) byte {
	return sectorsPerTrack[track]
}

// TotalSectors returns the total amount of sectors of this track.
func (t *Track) TotalSectors() byte {
	return totalSectors(t.ID)
}

// LoadDisk loads an existing disk from path and returns an initialized *Disk.
func LoadDisk(path string) (*Disk, error) {
	d := &Disk{SectorInterleave: DefaultSectorInterleave}
	data, err := os.ReadFile(path)
	if err != nil {
		return d, fmt.Errorf("os.ReadFile %q failed: %v", path, err)
	}
	d.loadDisk = data

	d.Tracks = make([]Track, MaxTracks)
	for track := byte(1); track <= MaxTracks; track++ {
		d.FormatTrack(track)

		for sector := byte(0); sector < d.Tracks[track-1].TotalSectors(); sector++ {
			offset := trackSectorToDataOffset(track, sector)
			for i := 0; i < 256; i++ {
				d.Tracks[track-1].Sectors[sector].Data[i] = data[offset+i]
			}
		}
	}
	d.loadBAM()
	d.guessInterleave()
	return d, nil
}

// trackSectorToDataOffset returns the offset in the .d64 of the given track and sector.
func trackSectorToDataOffset(track, sector byte) int {
	offset := int(sector) * 256
	for t := byte(1); t < track; t++ {
		offset += int(totalSectors(t)) * 256
	}
	return offset
}

// NewDisk returns a new formatted *Disk.
func NewDisk(name string, interleave byte) *Disk {
	d := &Disk{
		Name:             name,
		SectorInterleave: interleave,
		Tracks:           make([]Track, MaxTracks),
	}
	for track := byte(1); track <= MaxTracks; track++ {
		d.FormatTrack(track)
	}
	d.FormatDirectory()
	d.FormatBAM()
	d.setBamEntries()
	return d
}

// WriteTo writes the disk to io.Writer, implementing the io.WriterTo interface.
func (d *Disk) WriteTo(w io.Writer) (int64, error) {
	var n int64
	for _, t := range d.Tracks {
		for _, s := range t.Sectors {
			m, err := w.Write(s.Data[:])
			n += int64(m)
			if err != nil {
				return n, fmt.Errorf("w.Write failed: %v", err)
			}
		}
	}
	return n, nil
}

// WriteFile writes the disk to path.
func (d *Disk) WriteFile(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("os.Create %q failed: %v", path, err)
	}
	defer f.Close()
	if _, err = d.WriteTo(f); err != nil {
		return fmt.Errorf("d.WriteTo %q failed: %v", path, err)
	}
	return nil
}

// FormatTrack zerofills the track and marks its sectors free in d.bam.
func (d *Disk) FormatTrack(id byte) {
	t := Track{ID: id}
	t.Sectors = make([]Sector, t.TotalSectors())
	for i := byte(0); i < t.TotalSectors(); i++ {
		t.Sectors[i] = Sector{ID: i}
		d.bam[id-1][i] = false
	}
	d.Tracks[t.ID-1] = t
}

// FormatBAM formats track 18 sector 0, sets disk name and initializes the BAM.
func (d *Disk) FormatBAM() {
	var s Sector
	s.SetTrackLink(DirTrack)
	s.SetSectorLink(1)
	s.Data[2] = byte('A')

	for i := 0; i < 0x1a; i++ {
		s.Data[0x90+byte(i)] = 0xa0
	}

	for i, c := range strings.ToUpper(d.Name) {
		s.Data[0x90+byte(i)] = byte(c)
	}

	diskID := strings.ToUpper("votox")
	for i, c := range diskID {
		s.Data[i+0xa2] = byte(c)
	}

	d.Tracks[DirTrack-1].Sectors[0] = s
	d.bam[DirTrack-1][0] = true
	d.prepareBam()
}

// setNameFromBAM sets d.Name according to the data found in the BAM sector.
func (d *Disk) setNameFromBAM() {
	s := d.Tracks[DirTrack-1].Sectors[0]
	buf := [MaxFilenameSize]byte{}
	for i := 0; i < MaxFilenameSize; i++ {
		buf[i] = s.Data[0x90+i]
	}
	var name string
	for i := range buf {
		if buf[i] == 0xa0 {
			break
		}
		name += string(buf[i])
	}
	d.Name = strings.ToLower(name)
}

var reStripSlashes = regexp.MustCompile("[/]")

// ExtractToPath writes all files to outDir.
func (d *Disk) ExtractToPath(outDir string) error {
	for _, e := range d.Directory() {
		path := filepath.Join(outDir, reStripSlashes.ReplaceAllString(e.Filename, "")+".prg")
		if err := os.WriteFile(path, d.Extract(e.Track, e.Sector), 0644); err != nil {
			return fmt.Errorf("os.WriteFile %q to %q failed: %v", e.Filename, outDir, err)
		}
		d.Files = append(d.Files, path)
	}
	return nil
}

// Extract returns the prg starting on track, sector.
func (d *Disk) Extract(track, sector byte) (prg []byte) {
	for {
		s := d.Tracks[track-1].Sectors[sector]
		prg = append(prg, s.Content()...)
		if s.TrackLink() == 0 {
			break
		}
		track, sector = s.TrackLink(), s.SectorLink()
	}
	return prg
}

// guessInterleave iterates over all files on disk and sets d.SectorInterleave when guessing is easy.
func (d *Disk) guessInterleave() {
	for _, e := range d.Directory() {
		s := d.Tracks[e.Track-1].Sectors[e.Sector]
		if e.Track == s.TrackLink() && e.Sector < s.SectorLink() {
			d.SectorInterleave = s.SectorLink() - e.Sector
			return
		}
	}
}

// directoryEntries returns the DirEntries of a specific (directory) sector.
func (s *Sector) directoryEntries() (dirEntries []DirEntry) {
	for i := 2; i < 0xff; i += 32 {
		if s.Data[i] != 0x82 && s.Data[i] != 0xc2 {
			continue
		}
		var filename string
		for j := 0; j < MaxFilenameSize; j++ {
			if s.Data[i+3+j] == 0xa0 {
				break
			}
			filename += string(s.Data[i+3+j])
		}
		track, sector := s.Data[i+1], s.Data[i+2]
		if track < 1 || track > MaxTracks {
			continue
		}
		if sector >= totalSectors(track) {
			continue
		}
		dirEntries = append(dirEntries, DirEntry{
			Filename:  NormalizeFilename(filename),
			Track:     track,
			Sector:    sector,
			BlockSize: int(s.Data[i+28]) + int(s.Data[i+29])*0x100,
		})
	}
	return dirEntries
}

// addFileToDirectory adds filename to the directory, allocates a new sector if current ones are fully used.
func (d *Disk) addFileToDirectory(firstTrack, firstSector byte, filename string, prgLength int) error {
	name := strings.ToUpper(filename)
	if len(name) > MaxFilenameSize {
		return fmt.Errorf("name %q too long", name)
	}
	defer d.setBamEntries()
	track, sector := DirTrack, 1

	// find empty spot in current dir sectors
	for k := 0; k < 20; k++ {
		s := d.Tracks[track-1].Sectors[sector]
		for i := 2; i < 0xff; i += 32 {
			if s.Data[i] == 0x82 || s.Data[i] == 0xc2 {
				continue
			}
			// insert file
			s.Data[i] = 0x82
			s.Data[i+1] = firstTrack
			s.Data[i+2] = firstSector
			for j, v := range name {
				s.Data[i+3+j] = byte(v)
			}

			for j := len(name); j < MaxFilenameSize; j++ {
				s.Data[i+3+j] = 0xa0
			}
			b := SizeToBlocks(prgLength)
			s.Data[i+28] = byte(b) & 0xff
			s.Data[i+29] = byte((b & 0xff00) >> 8)

			d.Tracks[track-1].Sectors[sector] = s
			return nil
		}

		if s.TrackLink() == 0 {
			break
		}
		track, sector = int(s.TrackLink()), int(s.SectorLink())
	}

	// allocate new dir sector
	nextTrack, nextSector, err := d.nextFreeSector(byte(track), byte(sector))
	if err != nil {
		return fmt.Errorf("d.nextFreeSector for dir entry %q failed: %v", name, err)

	}
	d.Tracks[track-1].Sectors[sector].Data[0] = nextTrack
	d.Tracks[track-1].Sectors[sector].Data[1] = nextSector

	track, sector = int(nextTrack), int(nextSector)
	d.bam[track-1][sector] = true
	d.Tracks[track-1].Sectors[sector].Data[0] = 0x00
	d.Tracks[track-1].Sectors[sector].Data[1] = 0xff

	return d.addFileToDirectory(firstTrack, firstSector, filename, prgLength)
}

// FormatDirectory formats the first directory sector and allocates it in d.bam.
func (d *Disk) FormatDirectory() {
	s := Sector{ID: 1}
	s.SetTrackLink(0)
	s.SetSectorLink(0xff)
	d.Tracks[DirTrack-1].Sectors[s.ID] = s
	d.bam[DirTrack-1][1] = true
}

// Directory scans the dirtrack and returns all .prg DirEntries.
func (d *Disk) Directory() (dir []DirEntry) {
	dirSectors := make([]Sector, 0, 20)
	track, sector := byte(DirTrack), byte(1)
	for i := 0; i < 20; i++ {
		s := d.Tracks[track-1].Sectors[sector]
		dirSectors = append(dirSectors, s)
		if s.TrackLink() == 0 {
			break
		}
		track, sector = s.TrackLink(), s.SectorLink()
	}

	for _, s := range dirSectors {
		dir = append(dir, s.directoryEntries()...)
	}
	return dir
}

// Validate scans the directory, traces all files including dir, marks their sectors as used and updates the d.bam and the BAM sector.
func (d *Disk) Validate() {
	d.bam = [MaxTracks][MaxSectorsForBam]bool{}
	dirEntries := append(d.Directory(), DirEntry{Track: DirTrack, Sector: 0})
	for _, dirEntry := range dirEntries {
		track, sector := dirEntry.Track, dirEntry.Sector
		for {
			s := d.Tracks[track-1].Sectors[sector]
			d.bam[track-1][sector] = true
			if s.TrackLink() == 0 {
				break
			}
			track, sector = s.TrackLink(), s.SectorLink()
		}
	}
	d.setBamEntries()
	return
}

// printBam prints bam to stdout.
func printBam(bam [MaxTracks][MaxSectorsForBam]bool) {
	for i := 0; i < MaxTracks; i++ {
		track := i + 1
		fmt.Printf("track %2d: ", track)
		for sector := range bam[i] {
			used := "0"
			if bam[i][sector] == true {
				used = "1"
			}
			fmt.Printf(" %s", used)
		}
		fmt.Println()
	}
}

// setBamEntries calculates and sets all BAM entries according to d.bam.
func (d *Disk) setBamEntries() {
	d.prepareBam()
	var bamEntries []byte
	for track := byte(1); track <= MaxTracks; track++ {
		bamBytes := [3]byte{}
		total := totalSectors(track)
		freeSectors := total
		for sector := byte(0); sector < MaxSectorsForBam; sector++ {
			if d.bam[track-1][sector] {
				if sector < total {
					freeSectors--
				}
				bamBytes[sector/8] |= byte(1 << (sector % 8))
			}
		}
		for i := range bamBytes {
			bamBytes[i] = bamBytes[i] ^ 0xff
		}
		bamEntries = append(bamEntries, freeSectors)
		bamEntries = append(bamEntries, bamBytes[:]...)
	}
	for i, b := range bamEntries {
		d.Tracks[DirTrack-1].Sectors[0].Data[i+4] = b
	}
}

// prepareBam sets impossible sectors to true (used) in d.bam.
func (d *Disk) prepareBam() {
	for track := byte(1); track <= MaxTracks; track++ {
		total := totalSectors(track)
		for sector := byte(total); sector < MaxSectorsForBam; sector++ {
			d.bam[track-1][sector] = true
		}
	}
}

// loadBAM sets d.bam and d.Name according to the BAM entries on the disk.
func (d *Disk) loadBAM() {
	d.setNameFromBAM()
	bam := d.Tracks[DirTrack-1].Sectors[0]
	track := byte(0)
	for i := 4; i < (MaxTracks*4)+4; i += 4 {
		track++
		freeSectors := totalSectors(track)
		bamBytes := [3]byte{}
		for j := range bamBytes {
			bamBytes[j] = bam.Data[i+j+1]
		}
		for sector := byte(0); sector < totalSectors(track); sector++ {
			d.bam[track-1][sector] = false
			if bamBytes[sector/8]&(1<<(sector%8)) == 0 {
				freeSectors--
				d.bam[track-1][sector] = true
			}
		}
	}
}

// freeSector returns the first unallocated sector on the disk.
// returns error if the disk is full.
func (d *Disk) freeSector() (track, sector byte, err error) {
	for track = 1; track <= MaxTracks; track++ {
		if track == DirTrack {
			continue
		}
		for sector = 0; sector < totalSectors(track); sector++ {
			if d.bam[track-1][sector] == false {
				return track, sector, nil
			}
		}
	}
	return track, sector, fmt.Errorf("freeSector failed: disk full")
}

// nextFreeSector returns the next unallocated sector, taking SectorInterleave into account.
// It will skip over the DirTrack, unless you are trying to find the next directory sector.
func (d *Disk) nextFreeSector(currentTrack, currentSector byte) (track, sector byte, err error) {
	interleave := byte(d.SectorInterleave)
	dirSector := currentTrack == DirTrack
	if dirSector {
		interleave = DirInterleave
	}
	for track = currentTrack; track <= MaxTracks; track++ {
		if !dirSector && track == DirTrack {
			continue
		}

		currentSector = (currentSector + interleave) % totalSectors(track)
		for sector = currentSector; sector < totalSectors(track); sector++ {
			if d.bam[track-1][sector] == false {
				return track, sector, nil
			}
		}
		for sector = 0; sector < totalSectors(track); sector++ {
			if d.bam[track-1][sector] == false {
				return track, sector, nil
			}
		}
	}
	return track, sector, fmt.Errorf("nextFreeSector failed: disk full")
}

// AddFile reads the file at path and adds the file on the disk with filename.
func (d *Disk) AddFile(path, filename string) error {
	prg, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("os.ReadFile %q failed: %v", path, err)
	}
	return d.AddPrg(NormalizeFilename(filename), prg)
}

// AddPrg adds the prg to the disk with filename.
func (d *Disk) AddPrg(filename string, prg []byte) error {
	if len(prg) == 0 {
		return fmt.Errorf("prg file is empty")
	}
	track, sector, err := d.freeSector()
	if err != nil {
		return fmt.Errorf("d.freeSector failed: %v", err)
	}

	if err = d.addFileToDirectory(track, sector, filename, len(prg)); err != nil {
		return fmt.Errorf("d.addFileToDirectory %q failed: %v", filename, err)
	}

	buf := make([]byte, len(prg), len(prg))
	copy(buf, prg)

	// write full sectors
	for len(buf) > BlockSize {
		d.bam[track-1][sector] = true

		var sectorContent []byte
		sectorContent, buf = buf[0:BlockSize], buf[BlockSize:]

		nextTrack, nextSector, err := d.nextFreeSector(track, sector)
		if err != nil {
			return fmt.Errorf("d.nextFreeSector track %d sector %d failed: %v", track, sector, err)
		}

		d.Tracks[track-1].Sectors[sector].Data[0] = nextTrack
		d.Tracks[track-1].Sectors[sector].Data[1] = nextSector
		for i, v := range sectorContent {
			d.Tracks[track-1].Sectors[sector].Data[2+i] = v
		}

		track, sector = nextTrack, nextSector
	}

	// write partial last sector
	d.Tracks[track-1].Sectors[sector].Data[0] = 0
	d.Tracks[track-1].Sectors[sector].Data[1] = byte(len(buf) + 1)
	for i, v := range buf {
		d.Tracks[track-1].Sectors[sector].Data[2+i] = v
	}
	d.bam[track-1][sector] = true

	d.setBamEntries()
	return nil
}

var re = regexp.MustCompile("[^0-9a-z ./\\-_+\\[\\]]")

// NormalizeFilename trims and normalizes a filename to fit .d64 restrictions.
func NormalizeFilename(f string) string {
	n := strings.TrimSpace(re.ReplaceAllString(strings.ToLower(f), ""))
	if len(n) > MaxFilenameSize {
		return strings.TrimSpace(n[:MaxFilenameSize])
	}
	return n
}

// SizeToBlocks returns the amount of blocks (sectors) used for a file of specified size.
func SizeToBlocks(size int) (b int) {
	return int(size/BlockSize) + 1
}
