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
	MaxFilenameSize = 16
	MaxDiskIDSize   = 5

	MaxTracks               = 35
	DirTrack                = 18
	SectorSize              = 0x100
	DefaultSectorInterleave = 10
	DirInterleave           = 3
	MaxSectorsForBam        = 24
	FileIDMask              = 0b10111111
	PrgFileID               = 0x82
	AlternateSpaceCharacter = 0xa0
)

// A Sector represents a single sector.
type Sector struct {
	ID   byte
	Data [SectorSize]byte
}

// A Track represents a single track, consisting of multiple Sectors.
type Track struct {
	ID      byte
	Sectors []Sector
}

// A Disk represents a .d64 image.
type Disk struct {
	Label            string
	DiskID           string
	Tracks           []Track
	SectorInterleave byte
	bam              [MaxTracks][MaxSectorsForBam]bool
}

// A DirEntry represents a single file in the directory of this d64.
type DirEntry struct {
	Track     byte
	Sector    byte
	Filename  string
	BlockSize int
}

// TrackLink returns this sectors next track-link.
func (s Sector) TrackLink() byte {
	return s.Data[0]
}

// SetTrackLink sets the track-link for the next sector of this file.
func (s *Sector) SetTrackLink(b byte) {
	s.Data[0] = b
}

// SectorLink returns this sectors next sector-link.
func (s Sector) SectorLink() byte {
	return s.Data[1]
}

// SetSectorLink sets the sector-link for the next sector of this file.
func (s *Sector) SetSectorLink(b byte) {
	s.Data[1] = b
}

// Bytes returns the binary content of this sector, track&sector link are not included.
func (s Sector) Bytes() []byte {
	if s.TrackLink() == 0 && s.SectorLink()+1 != 0 {
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
func (t Track) TotalSectors() byte {
	return totalSectors(t.ID)
}

// LoadDisk loads an existing disk from path and returns an initialized *Disk.
func LoadDisk(path string) (*Disk, error) {
	d := &Disk{SectorInterleave: DefaultSectorInterleave}
	bin, err := os.ReadFile(path)
	if err != nil {
		return d, fmt.Errorf("os.ReadFile %q failed: %w", path, err)
	}

	d.Tracks = make([]Track, MaxTracks)
	for track := byte(1); track <= MaxTracks; track++ {
		d.FormatTrack(track)
		for sector := byte(0); sector < d.Tracks[track-1].TotalSectors(); sector++ {
			offset := trackSectorToDataOffset(track, sector)
			for i := 0; i < SectorSize; i++ {
				d.Tracks[track-1].Sectors[sector].Data[i] = bin[offset+i]
			}
		}
	}
	d.loadBAM()
	d.guessInterleave()
	return d, nil
}

// trackSectorToDataOffset returns the offset in the .d64 of the given track and sector.
func trackSectorToDataOffset(track, sector byte) int {
	offset := int(sector) * SectorSize
	for t := byte(1); t < track; t++ {
		offset += int(totalSectors(t)) * SectorSize
	}
	return offset
}

// NewDisk returns a new formatted *Disk.
func NewDisk(label, diskID string, interleave byte) *Disk {
	d := &Disk{
		Label:            label,
		DiskID:           diskID,
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

// String implements the Stringer interface and returns a human readable directory.
func (d Disk) String() string {
	s := fmt.Sprintf("%q %q\n", d.Label, d.DiskID)
	blocksFree := MaxBlocks
	for _, e := range d.Directory() {
		s += fmt.Sprintf("%3d %q prg\n", e.BlockSize, e.Filename)
		blocksFree -= e.BlockSize
	}
	s += fmt.Sprintf("%3d blocks free\n", blocksFree)
	return s
}

// WriteTo writes the disk to io.Writer, implementing the io.WriterTo interface.
func (d Disk) WriteTo(w io.Writer) (int64, error) {
	var n int64
	for _, t := range d.Tracks {
		for _, s := range t.Sectors {
			m, err := w.Write(s.Data[:])
			n += int64(m)
			if err != nil {
				return n, fmt.Errorf("w.Write failed: %w", err)
			}
		}
	}
	return n, nil
}

// WriteFile writes the disk to path.
func (d Disk) WriteFile(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("os.Create %q failed: %w", path, err)
	}
	defer f.Close()
	if _, err = d.WriteTo(f); err != nil {
		return fmt.Errorf("d.WriteTo %q failed: %w", path, err)
	}
	return nil
}

// FormatTrack zerofills the track and marks its sectors free in d.bam.
func (d *Disk) FormatTrack(id byte) {
	t := Track{ID: id}
	t.Sectors = make([]Sector, t.TotalSectors())
	for i := byte(0); i < byte(len(t.Sectors)); i++ {
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
		s.Data[0x90+byte(i)] = AlternateSpaceCharacter
	}

	if len(d.Label) > MaxFilenameSize {
		d.Label = d.Label[0:MaxFilenameSize]
	}
	for i, c := range strings.ToUpper(d.Label) {
		s.Data[0x90+byte(i)] = byte(c)
	}

	if len(d.DiskID) > MaxDiskIDSize {
		d.DiskID = d.DiskID[0:MaxDiskIDSize]
	}
	for i, c := range strings.ToUpper(d.DiskID) {
		if c == ' ' {
			c = AlternateSpaceCharacter
		}
		s.Data[i+0xa2] = byte(c)
	}

	d.Tracks[DirTrack-1].Sectors[0] = s
	d.bam[DirTrack-1][0] = true
	d.prepareBam()
}

// setLabelFromBAM sets d.Label according to the data found in the BAM sector.
func (d *Disk) setLabelFromBAM() {
	buf := [MaxFilenameSize]byte{}
	for i := 0; i < MaxFilenameSize; i++ {
		buf[i] = d.Tracks[DirTrack-1].Sectors[0].Data[0x90+i]
	}
	d.Label = ""
	for i := range buf {
		if buf[i] == AlternateSpaceCharacter {
			break
		}
		d.Label += strings.ToLower(string(buf[i]))
	}
}

// setDiskIDFromBAM sets d.DiskID according to the data found in the BAM sector.
func (d *Disk) setDiskIDFromBAM() {
	buf := [MaxDiskIDSize]byte{}
	for i := 0; i < MaxDiskIDSize; i++ {
		buf[i] = d.Tracks[DirTrack-1].Sectors[0].Data[0xa2+i]
	}
	d.DiskID = ""
	for i := range buf {
		if buf[i] == AlternateSpaceCharacter {
			buf[i] = ' '
		}
		d.DiskID += strings.ToLower(string(buf[i]))
	}
}

var reStripSlashes = regexp.MustCompile("[/]")

// ExtractToPath writes all files to outDir and returns a slice containing all paths.
func (d *Disk) ExtractToPath(outDir string) (paths []string, err error) {
	for i, e := range d.Directory() {
		filename := reStripSlashes.ReplaceAllString(e.Filename, "")
		if filename == "" {
			filename = fmt.Sprintf("file%d", i)
		}
		path := filepath.Join(outDir, filename+".prg")
		if err = os.WriteFile(path, d.Extract(e.Track, e.Sector), 0644); err != nil {
			return paths, fmt.Errorf("os.WriteFile %q to %q failed: %w", e.Filename, outDir, err)
		}
		paths = append(paths, path)
	}
	return paths, nil
}

// Extract returns the prg starting on track, sector.
func (d Disk) Extract(track, sector byte) (prg []byte) {
	for {
		s := d.Tracks[track-1].Sectors[sector]
		prg = append(prg, s.Bytes()...)
		if s.TrackLink() == 0 {
			break
		}
		track, sector = s.TrackLink(), s.SectorLink()
	}
	return prg
}

// ExtractBoot returns the first prg found in the directory.
func (d Disk) ExtractBoot() (prg []byte) {
	boot := d.Directory()[0]
	return d.Extract(boot.Track, boot.Sector)
}

// guessInterleave iterates over all files on disk and sets d.SectorInterleave.
func (d *Disk) guessInterleave() {
	d.SectorInterleave = DefaultSectorInterleave
	for _, e := range d.Directory() {
		s := d.Tracks[e.Track-1].Sectors[e.Sector]
		if e.Track == s.TrackLink() && e.Sector < s.SectorLink() {
			d.SectorInterleave = s.SectorLink() - e.Sector
			return
		}
	}
}

// directoryEntries returns the DirEntries of a specific (directory) sector.
func (s Sector) directoryEntries() (dirEntries []DirEntry) {
	for i := 2; i < SectorSize; i += 32 {
		if s.Data[i]&FileIDMask != PrgFileID {
			continue
		}
		var filename string
		for j := 0; j < MaxFilenameSize; j++ {
			if s.Data[i+3+j] == AlternateSpaceCharacter {
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
			BlockSize: int(s.Data[i+28]) + int(s.Data[i+29])<<8,
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
	for k := 0; k < len(d.Tracks[track-1].Sectors); k++ {
		s := d.Tracks[track-1].Sectors[sector]
		for i := 2; i < 0xff; i += 32 {
			// keep prg files
			if s.Data[i]&FileIDMask == PrgFileID {
				continue
			}
			// insert file
			s.Data[i] = PrgFileID
			s.Data[i+1] = firstTrack
			s.Data[i+2] = firstSector
			for j, v := range name {
				s.Data[i+3+j] = byte(v)
			}

			for j := len(name); j < MaxFilenameSize; j++ {
				s.Data[i+3+j] = AlternateSpaceCharacter
			}
			b := SizeToBlocks(prgLength)
			s.Data[i+28] = byte(b) & 0xff
			s.Data[i+29] = byte(b >> 8)

			d.Tracks[track-1].Sectors[sector] = s
			return nil
		}

		if s.TrackLink() == 0 {
			break
		}
		track = int(s.TrackLink())
		sector = int(s.SectorLink())
	}

	// allocate new dir sector
	nextTrack, nextSector, err := d.nextFreeSector(byte(track), byte(sector))
	if err != nil {
		return fmt.Errorf("d.nextFreeSector for dir entry %q failed: %w", name, err)

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
	d.bam[DirTrack-1][s.ID] = true
}

// Directory scans the DirTrack and returns all .prg DirEntries.
func (d Disk) Directory() (dir []DirEntry) {
	track, sector := byte(DirTrack), byte(1)
	for i := byte(0); i < totalSectors(DirTrack); i++ {
		s := d.Tracks[track-1].Sectors[sector]
		dir = append(dir, s.directoryEntries()...)
		if s.TrackLink() == 0 {
			return dir
		}
		track, sector = s.TrackLink(), s.SectorLink()
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

// PrintBamTo prints a human readable representation of d.bam to the io.Writer.
// Typical usage is writing to os.Stdout.
func (d *Disk) PrintBamTo(w io.Writer) (n int, err error) {
	for track := 1; track <= MaxTracks; track++ {
		n, err = fmt.Fprintf(w, "track %2d: ", track)
		if err != nil {
			return n, fmt.Errorf("fmt.Fprintf failed: %w", err)
		}
		for sector := range d.bam[track-1] {
			used := "0"
			if d.bam[track-1][sector] == true {
				used = "1"
			}
			m, err := fmt.Fprintf(w, " %s", used)
			n += m
			if err != nil {
				return n, fmt.Errorf("fmt.Fprintf failed: %w", err)
			}
		}
		m, err := fmt.Fprintln(w)
		n += m
		if err != nil {
			return n, fmt.Errorf("fmt.Fprintf failed: %w", err)
		}
	}
	return n, nil
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
		for sector := totalSectors(track); sector < MaxSectorsForBam; sector++ {
			d.bam[track-1][sector] = true
		}
	}
}

// loadBAM sets d.bam and d.Label according to the BAM entries on the disk.
func (d *Disk) loadBAM() {
	d.setLabelFromBAM()
	d.setDiskIDFromBAM()
	bam := d.Tracks[DirTrack-1].Sectors[0]
	track := byte(0)
	for i := 4; i < (MaxTracks*4)+4; i += 4 {
		track++
		bamBytes := [3]byte{}
		for j := range bamBytes {
			bamBytes[j] = bam.Data[i+j+1]
		}
		for sector := byte(0); sector < totalSectors(track); sector++ {
			d.bam[track-1][sector] = false
			if bamBytes[sector/8]&(1<<(sector%8)) == 0 {
				d.bam[track-1][sector] = true
			}
		}
	}
}

// freeSector returns the first unallocated sector on the disk.
// returns error if the disk is full.
func (d Disk) freeSector() (track, sector byte, err error) {
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
func (d Disk) nextFreeSector(currentTrack, currentSector byte) (track, sector byte, err error) {
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
		return fmt.Errorf("os.ReadFile %q failed: %w", path, err)
	}
	return d.AddPrg(NormalizeFilename(filename), prg)
}

func (d *Disk) AddPrgFromReader(filename string, r io.Reader) error {
	buf, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("io.ReadAll %q failed: %w", filename, err)
	}
	return d.AddPrg(filename, buf)
}

// AddPrg adds the prg to the disk with filename.
func (d *Disk) AddPrg(filename string, prg []byte) error {
	if len(prg) == 0 {
		return fmt.Errorf("prg file is empty")
	}
	track, sector, err := d.freeSector()
	if err != nil {
		return fmt.Errorf("d.freeSector failed: %w", err)
	}

	if err = d.addFileToDirectory(track, sector, filename, len(prg)); err != nil {
		return fmt.Errorf("d.addFileToDirectory %q failed: %w", filename, err)
	}

	buf := make([]byte, len(prg), len(prg))
	copy(buf, prg)

	// drain buffer with writing full sectors
	for len(buf) > BlockSize {
		d.bam[track-1][sector] = true

		var sectorContent []byte
		sectorContent, buf = buf[0:BlockSize], buf[BlockSize:]

		nextTrack, nextSector, err := d.nextFreeSector(track, sector)
		if err != nil {
			return fmt.Errorf("d.nextFreeSector track %d sector %d failed: %w", track, sector, err)
		}

		d.Tracks[track-1].Sectors[sector].Data[0] = nextTrack
		d.Tracks[track-1].Sectors[sector].Data[1] = nextSector
		for i, v := range sectorContent {
			d.Tracks[track-1].Sectors[sector].Data[2+i] = v
		}

		track, sector = nextTrack, nextSector
	}

	if len(buf) > 0 {
		// write partial last sector
		d.bam[track-1][sector] = true
		d.Tracks[track-1].Sectors[sector].Data[0] = 0
		d.Tracks[track-1].Sectors[sector].Data[1] = byte(len(buf) + 1)
		for i, v := range buf {
			d.Tracks[track-1].Sectors[sector].Data[2+i] = v
		}
	}

	d.setBamEntries()
	return nil
}

var re = regexp.MustCompile("[^0-9a-z ._+]")

// NormalizeFilename trims and normalizes a filename to fit .d64 restrictions.
func NormalizeFilename(f string) string {
	n := strings.TrimSpace(re.ReplaceAllString(strings.ToLower(f), ""))
	if n == "." || n == ".." {
		return "dot"
	}
	if len(n) > MaxFilenameSize {
		return strings.TrimSpace(n[:MaxFilenameSize])
	}
	return n
}

// SizeToBlocks returns the amount of blocks (sectors) used for a file of specified size.
func SizeToBlocks(size int) (b int) {
	return int(size/BlockSize) + 1
}
