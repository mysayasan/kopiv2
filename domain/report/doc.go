// Package report is a small, dependency-light builder for the printable PDF reports
// the fleet apps generate on demand. It wraps go-pdf/fpdf so report code reads as
// data (headings, key/value blocks, stat tiles, tables, embedded images) rather than
// a stream of low-level cell calls, and it centralises the shared look — the branded
// header band, the zebra tables, the "page X of Y" footer — so every report across
// the suite comes out consistent.
//
// It is pure Go with no external binary: the apps that use it (myseliasan first) run
// air-gapped, so rendering a report can never reach for a headless browser or a
// system font. Text uses fpdf's built-in Helvetica core font through the Windows-1252
// translator, which covers Latin scripts (English, Malay, accented European). CJK and
// Arabic in user-entered names render blank until a Unicode TTF is bundled — a
// deliberate v1 limitation, not a silent failure.
package report

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"time"

	"github.com/go-pdf/fpdf"
)

// Palette is the small colour set the shared look is drawn from. Callers may override
// Primary to tint a report for a specific app (myseliasan's steel blue by default).
type Palette struct {
	Primary [3]int // header band + accents
	Ink     [3]int // body text
	Muted   [3]int // secondary text
	Zebra   [3]int // alternating table row fill
	Border  [3]int // hairlines
	TileBg  [3]int // stat-tile fill
}

// DefaultPalette is myseliasan's steel-blue scheme.
func DefaultPalette() Palette {
	return Palette{
		Primary: [3]int{43, 108, 176},  // #2B6CB0
		Ink:     [3]int{26, 32, 44},     // #1A202C
		Muted:   [3]int{113, 128, 150},  // #718096
		Zebra:   [3]int{247, 250, 252},  // #F7FAFC
		Border:  [3]int{226, 232, 240},  // #E2E8F0
		TileBg:  [3]int{237, 242, 247},  // #EDF2F7
	}
}

// Options carry the cover/header metadata every report shows. GeneratedAt is passed in
// by the caller (not read from the clock here) so report output is deterministic and
// unit-testable.
type Options struct {
	Title       string    // e.g. "Fleet Health Report"
	Subtitle    string    // e.g. a site name, or the range in words
	Period      string    // e.g. "Last 30 days" (blank to omit)
	GeneratedAt time.Time
	AppName     string  // footer attribution, e.g. "myseliasan"
	Palette     Palette // zero value → DefaultPalette
}

// Document is a report under construction. Build it with the content helpers, then
// call Output to get the PDF bytes.
type Document struct {
	pdf *fpdf.Fpdf
	tr  func(string) string
	pal Palette
	opt Options
	// contentTop is the Y (mm) below the header band where body content may start on
	// every page; kept so page breaks land clear of the band.
	contentTop float64
}

const (
	pageMarginL = 14.0
	pageMarginR = 14.0
	pageMarginT = 24.0 // leaves room for the header band drawn by the header func
	pageMarginB = 16.0
	headerBandH = 18.0
)

// New starts a report. The first page is added ready for content.
func New(opt Options) *Document {
	pal := opt.Palette
	if (pal == Palette{}) {
		pal = DefaultPalette()
	}
	if opt.AppName == "" {
		opt.AppName = "myseliasan"
	}
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(pageMarginL, pageMarginT, pageMarginR)
	pdf.SetAutoPageBreak(true, pageMarginB)
	pdf.AliasNbPages("") // enables {nb} = total page count in the footer

	d := &Document{
		pdf:        pdf,
		tr:         pdf.UnicodeTranslatorFromDescriptor(""), // cp1252
		pal:        pal,
		opt:        opt,
		contentTop: pageMarginT,
	}
	pdf.SetHeaderFunc(d.drawHeader)
	pdf.SetFooterFunc(d.drawFooter)
	pdf.AddPage()
	return d
}

// t sanitises a UTF-8 string into the PDF's cp1252 encoding, dropping glyphs the core
// font cannot represent rather than panicking on them.
func (d *Document) t(s string) string { return d.tr(s) }

func (d *Document) setInk()   { d.pdf.SetTextColor(d.pal.Ink[0], d.pal.Ink[1], d.pal.Ink[2]) }
func (d *Document) setMuted() { d.pdf.SetTextColor(d.pal.Muted[0], d.pal.Muted[1], d.pal.Muted[2]) }

// drawHeader renders the branded band at the top of every page.
func (d *Document) drawHeader() {
	p := d.pdf
	w, _ := p.GetPageSize()
	p.SetFillColor(d.pal.Primary[0], d.pal.Primary[1], d.pal.Primary[2])
	p.Rect(0, 0, w, headerBandH, "F")

	// Title (left).
	p.SetXY(pageMarginL, 4)
	p.SetTextColor(255, 255, 255)
	p.SetFont("Helvetica", "B", 13)
	p.CellFormat(w/2, 6, d.t(d.opt.Title), "", 2, "L", false, 0, "")
	if d.opt.Subtitle != "" {
		p.SetFont("Helvetica", "", 9)
		p.CellFormat(w/2, 5, d.t(d.opt.Subtitle), "", 0, "L", false, 0, "")
	}

	// Generated-at + period (right).
	p.SetFont("Helvetica", "", 8)
	right := fmt.Sprintf("Generated %s", d.opt.GeneratedAt.Format("2006-01-02 15:04"))
	p.SetXY(w/2, 4)
	p.CellFormat(w/2-pageMarginR, 5, d.t(right), "", 2, "R", false, 0, "")
	if d.opt.Period != "" {
		p.CellFormat(w/2-pageMarginR, 5, d.t(d.opt.Period), "", 0, "R", false, 0, "")
	}

	p.SetXY(pageMarginL, d.contentTop)
	d.setInk()
}

// drawFooter renders the hairline + "app · page X of Y" line on every page.
func (d *Document) drawFooter() {
	p := d.pdf
	w, h := p.GetPageSize()
	p.SetY(-pageMarginB + 4)
	p.SetDrawColor(d.pal.Border[0], d.pal.Border[1], d.pal.Border[2])
	p.Line(pageMarginL, h-pageMarginB+2, w-pageMarginR, h-pageMarginB+2)
	p.SetFont("Helvetica", "", 8)
	d.setMuted()
	p.CellFormat((w-pageMarginL-pageMarginR)/2, 6, d.t(d.opt.AppName+" · confidential"), "", 0, "L", false, 0, "")
	p.CellFormat((w-pageMarginL-pageMarginR)/2, 6, fmt.Sprintf("Page %d of {nb}", p.PageNo()), "", 0, "R", false, 0, "")
}

// --- content helpers --------------------------------------------------------------

// H1 writes a primary section heading with an accent rule under it.
func (d *Document) H1(text string) {
	p := d.pdf
	d.ensureSpace(14)
	p.Ln(2)
	p.SetFont("Helvetica", "B", 14)
	p.SetTextColor(d.pal.Primary[0], d.pal.Primary[1], d.pal.Primary[2])
	p.CellFormat(0, 8, d.t(text), "", 1, "L", false, 0, "")
	x, y := pageMarginL, p.GetY()+0.5
	w, _ := p.GetPageSize()
	p.SetDrawColor(d.pal.Primary[0], d.pal.Primary[1], d.pal.Primary[2])
	p.Line(x, y, w-pageMarginR, y)
	p.Ln(3)
	d.setInk()
}

// H2 writes a secondary heading.
func (d *Document) H2(text string) {
	d.ensureSpace(10)
	d.pdf.Ln(1)
	d.pdf.SetFont("Helvetica", "B", 11)
	d.setInk()
	d.pdf.CellFormat(0, 6, d.t(text), "", 1, "L", false, 0, "")
	d.pdf.Ln(1)
}

// Para writes a wrapped paragraph of body text.
func (d *Document) Para(text string) {
	d.pdf.SetFont("Helvetica", "", 10)
	d.setInk()
	d.pdf.MultiCell(0, 5, d.t(text), "", "L", false)
	d.pdf.Ln(1)
}

// Note writes a muted, smaller caption line (footnotes, "as of" stamps, caveats).
func (d *Document) Note(text string) {
	d.pdf.SetFont("Helvetica", "I", 8)
	d.setMuted()
	d.pdf.MultiCell(0, 4, d.t(text), "", "L", false)
	d.setInk()
}

// Spacer adds vertical whitespace (mm).
func (d *Document) Spacer(mm float64) { d.pdf.Ln(mm) }

// AddPage starts a fresh page.
func (d *Document) AddPage() { d.pdf.AddPage() }

// KeyValues renders a two-column definition list (label : value), wrapping values.
func (d *Document) KeyValues(rows [][2]string) {
	p := d.pdf
	const labelW = 52.0
	w, _ := p.GetPageSize()
	valW := w - pageMarginL - pageMarginR - labelW
	for _, kv := range rows {
		d.ensureSpace(6)
		p.SetFont("Helvetica", "B", 9)
		d.setMuted()
		p.CellFormat(labelW, 5.5, d.t(kv[0]), "", 0, "L", false, 0, "")
		p.SetFont("Helvetica", "", 10)
		d.setInk()
		x, y := p.GetX(), p.GetY()
		p.MultiCell(valW, 5.5, d.t(kv[1]), "", "L", false)
		// Keep single-line rows tidy; MultiCell already advanced Y for multi-line.
		_ = x
		_ = y
	}
	p.Ln(1)
}

// Tile is one summary metric on a stat-tile row.
type Tile struct {
	Label string
	Value string
	// Accent, when true, tints the value in the primary colour (use for the headline
	// number). Danger tints it red (for a bad count like certs-expired).
	Accent bool
	Danger bool
}

// StatTiles draws a row of up to 5 summary tiles. Rows of more than 5 wrap.
func (d *Document) StatTiles(tiles []Tile) {
	if len(tiles) == 0 {
		return
	}
	p := d.pdf
	w, _ := p.GetPageSize()
	const perRow = 4
	avail := w - pageMarginL - pageMarginR
	gap := 3.0
	for i := 0; i < len(tiles); i += perRow {
		end := i + perRow
		if end > len(tiles) {
			end = len(tiles)
		}
		n := end - i
		tw := (avail - gap*float64(perRow-1)) / float64(perRow)
		d.ensureSpace(20)
		x0 := pageMarginL
		y0 := p.GetY()
		for j := 0; j < n; j++ {
			x := x0 + float64(j)*(tw+gap)
			p.SetFillColor(d.pal.TileBg[0], d.pal.TileBg[1], d.pal.TileBg[2])
			p.SetDrawColor(d.pal.Border[0], d.pal.Border[1], d.pal.Border[2])
			p.RoundedRect(x, y0, tw, 18, 1.5, "1234", "FD")
			tile := tiles[i+j]
			p.SetXY(x, y0+2.5)
			p.SetFont("Helvetica", "B", 18)
			switch {
			case tile.Danger:
				p.SetTextColor(197, 48, 48) // red
			case tile.Accent:
				p.SetTextColor(d.pal.Primary[0], d.pal.Primary[1], d.pal.Primary[2])
			default:
				d.setInk()
			}
			p.CellFormat(tw, 9, d.t(tile.Value), "", 2, "C", false, 0, "")
			p.SetX(x)
			p.SetFont("Helvetica", "", 8)
			d.setMuted()
			p.CellFormat(tw, 4, d.t(tile.Label), "", 0, "C", false, 0, "")
		}
		p.SetY(y0 + 18 + 3)
	}
	d.setInk()
}

// Column describes one table column: header text, width in mm (0 → auto share of the
// remaining width), and alignment ("L", "C", "R").
type Column struct {
	Header string
	Width  float64
	Align  string
}

// Table renders a bordered, zebra-striped table with a coloured header row that
// repeats after a page break. Cells wrap; each row's height grows to its tallest cell.
func (d *Document) Table(cols []Column, rows [][]string) {
	if len(cols) == 0 {
		return
	}
	p := d.pdf
	w, _ := p.GetPageSize()
	avail := w - pageMarginL - pageMarginR

	// Resolve auto (0) widths to an even share of what fixed columns leave.
	widths := make([]float64, len(cols))
	fixed, autoN := 0.0, 0
	for i, c := range cols {
		widths[i] = c.Width
		if c.Width <= 0 {
			autoN++
		} else {
			fixed += c.Width
		}
	}
	if autoN > 0 {
		share := (avail - fixed) / float64(autoN)
		for i := range widths {
			if widths[i] <= 0 {
				widths[i] = share
			}
		}
	}

	drawHead := func() {
		p.SetFont("Helvetica", "B", 8.5)
		p.SetFillColor(d.pal.Primary[0], d.pal.Primary[1], d.pal.Primary[2])
		p.SetTextColor(255, 255, 255)
		p.SetDrawColor(d.pal.Border[0], d.pal.Border[1], d.pal.Border[2])
		for i, c := range cols {
			p.CellFormat(widths[i], 7, d.t(c.Header), "1", 0, alignOf(c.Align), true, 0, "")
		}
		p.Ln(-1)
	}

	d.ensureSpace(14)
	drawHead()

	p.SetFont("Helvetica", "", 8.5)
	lineH := 4.6
	for ri, row := range rows {
		// Height = tallest wrapped cell in this row.
		maxLines := 1
		for i := range cols {
			var txt string
			if i < len(row) {
				txt = row[i]
			}
			n := len(p.SplitLines([]byte(d.t(txt)), widths[i]-2))
			if n > maxLines {
				maxLines = n
			}
		}
		rowH := float64(maxLines) * lineH
		if rowH < 6 {
			rowH = 6
		}

		// Page-break check: repeat the header on the new page.
		if p.GetY()+rowH > d.pageBottom() {
			p.AddPage()
			drawHead()
			p.SetFont("Helvetica", "", 8.5)
		}

		fill := ri%2 == 1
		if fill {
			p.SetFillColor(d.pal.Zebra[0], d.pal.Zebra[1], d.pal.Zebra[2])
		}
		d.setInk()
		x, y := p.GetX(), p.GetY()
		for i, c := range cols {
			var txt string
			if i < len(row) {
				txt = row[i]
			}
			cx := p.GetX()
			// Border + fill box for the full row height, then the (possibly wrapped) text.
			p.Rect(cx, y, widths[i], rowH, map[bool]string{true: "FD", false: "D"}[fill])
			p.SetXY(cx, y)
			p.MultiCell(widths[i], lineH, d.t(txt), "", alignOf(c.Align), false)
			p.SetXY(cx+widths[i], y)
		}
		p.SetXY(x, y+rowH)
	}
	p.Ln(2)
}

func alignOf(a string) string {
	switch a {
	case "C", "R", "L":
		return a
	default:
		return "L"
	}
}

// Image embeds an image scaled to fit maxWidthMM, left-aligned, flowing the cursor
// below it. The aspect ratio is taken from the image's own bounds (not fpdf's image
// info), so embedding never depends on fpdf successfully re-parsing the encoded bytes
// for its dimensions. Returns any encode/registration error so callers can surface it
// rather than silently dropping the image.
func (d *Document) Image(name string, img image.Image, maxWidthMM float64) error {
	b := img.Bounds()
	natW, natH := b.Dx(), b.Dy()
	if natW <= 0 || natH <= 0 {
		return fmt.Errorf("report: image %q has zero size", name)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return fmt.Errorf("report: encode image %q: %w", name, err)
	}
	p := d.pdf
	// Register under a stable name; callers pass distinct names for distinct images.
	p.RegisterImageOptionsReader(name, fpdf.ImageOptions{ImageType: "PNG"}, &buf)
	if err := p.Error(); err != nil {
		p.ClearError()
		return fmt.Errorf("report: register image %q: %w", name, err)
	}
	wMM := maxWidthMM
	hMM := wMM * float64(natH) / float64(natW)
	// If the image is taller than the usable page, cap its width so the whole plan fits
	// on one page (better a smaller-but-complete plan than one clipped off the bottom).
	if maxH := d.pageBottom() - d.contentTop; hMM > maxH {
		scale := maxH / hMM
		wMM *= scale
		hMM = maxH
	}
	// Page-break if it would overflow the current page.
	if p.GetY()+hMM > d.pageBottom() {
		p.AddPage()
	}
	p.ImageOptions(name, p.GetX(), p.GetY(), wMM, hMM, true, fpdf.ImageOptions{ImageType: "PNG"}, 0, "")
	return nil
}

// --- layout guards ----------------------------------------------------------------

func (d *Document) pageBottom() float64 {
	_, h := d.pdf.GetPageSize()
	return h - pageMarginB
}

// ensureSpace starts a new page if fewer than mm millimetres remain, so a heading or
// tile row is never orphaned at the very bottom.
func (d *Document) ensureSpace(mm float64) {
	if d.pdf.GetY()+mm > d.pageBottom() {
		d.pdf.AddPage()
	}
}

// Output finalises the document and returns the PDF bytes.
func (d *Document) Output() ([]byte, error) {
	var buf bytes.Buffer
	if err := d.pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Empty writes a centred muted "nothing to report" line — used when a section has no
// rows, so the reader sees an explicit empty state rather than a missing section.
func (d *Document) Empty(text string) {
	d.pdf.SetFont("Helvetica", "I", 9)
	d.setMuted()
	d.pdf.CellFormat(0, 8, d.t(text), "", 1, "L", false, 0, "")
	d.setInk()
}
