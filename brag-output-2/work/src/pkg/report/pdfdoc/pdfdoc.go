// SPDX-License-Identifier: Apache-2.0

// Package pdfdoc renders report documents as PDF with go-pdf/fpdf. Fonts
// (DejaVu Sans Condensed, see fonts/LICENSE) are embedded, nothing refers to
// an external resource, and the output is deterministic: identical content
// and options give identical bytes.
package pdfdoc

import (
	"bytes"
	_ "embed"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
)

//go:embed fonts/DejaVuSansCondensed.ttf
var fontRegular []byte

//go:embed fonts/DejaVuSansCondensed-Bold.ttf
var fontBold []byte

const (
	family   = "dejavu"
	margin   = 15.0
	bodySize = 9.0
	lineH    = 4.6
)

// Options configures a document.
type Options struct {
	Title    string    // document title, also the PDF metadata title
	Subtitle string    // second header line, for example the scope
	Footer   string    // left footer text, for example the report ID and facts hash
	Notice   string    // short statement printed in every footer
	Date     time.Time // creation and modification date (determinism)
	// Watermark is drawn diagonally on every page when not empty.
	Watermark string
}

// Doc is a document being written.
type Doc struct {
	pdf  *fpdf.Fpdf
	opts Options
}

// New starts an A4 portrait document.
func New(o Options) *Doc {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetCatalogSort(true)
	pdf.SetCompression(true)
	pdf.SetCreationDate(o.Date.UTC())
	pdf.SetModificationDate(o.Date.UTC())
	pdf.SetProducer("compliance-engine", true)
	pdf.SetCreator("compliance-engine", true)
	pdf.SetTitle(o.Title, true)
	pdf.AddUTF8FontFromBytes(family, "", fontRegular)
	pdf.AddUTF8FontFromBytes(family, "B", fontBold)
	pdf.SetMargins(margin, margin+8, margin)
	pdf.SetAutoPageBreak(true, margin+6)
	pdf.AliasNbPages("{nb}")
	d := &Doc{pdf: pdf, opts: o}
	pdf.SetHeaderFunc(d.header)
	pdf.SetFooterFunc(d.footer)
	pdf.AddPage()
	return d
}

func (d *Doc) header() {
	p := d.pdf
	if d.opts.Watermark != "" {
		w, h := p.GetPageSize()
		p.SetFont(family, "B", 34)
		p.SetTextColor(225, 190, 190)
		tw := p.GetStringWidth(d.opts.Watermark)
		p.TransformBegin()
		p.TransformRotate(55, w/2, h/2)
		p.Text(w/2-tw/2, h/2, d.opts.Watermark)
		p.TransformEnd()
	}
	p.SetTextColor(0, 0, 0)
	p.SetXY(margin, 8)
	p.SetFont(family, "B", 8)
	p.CellFormat(0, 4, d.opts.Title, "", 1, "L", false, 0, "")
	p.SetFont(family, "", 7)
	p.CellFormat(0, 3.5, d.opts.Subtitle, "B", 1, "L", false, 0, "")
	p.SetXY(margin, margin+8)
	p.SetFont(family, "", bodySize)
}

func (d *Doc) footer() {
	p := d.pdf
	_, h := p.GetPageSize()
	p.SetXY(margin, h-margin)
	p.SetFont(family, "", 6.5)
	p.SetTextColor(70, 70, 70)
	p.CellFormat(150, 3, d.opts.Footer, "T", 0, "L", false, 0, "")
	p.CellFormat(0, 3, fmt.Sprintf("page %d/{nb}", p.PageNo()), "T", 1, "R", false, 0, "")
	p.CellFormat(0, 3, d.opts.Notice, "", 1, "L", false, 0, "")
	p.SetTextColor(0, 0, 0)
	p.SetFont(family, "", bodySize)
}

func (d *Doc) width() float64 {
	w, _ := d.pdf.GetPageSize()
	return w - 2*margin
}

// ensure starts a new page when less than h millimetres are left.
func (d *Doc) ensure(h float64) {
	_, ph := d.pdf.GetPageSize()
	_, bottom := d.pdf.GetAutoPageBreak()
	if d.pdf.GetY()+h > ph-bottom {
		d.pdf.AddPage()
	}
}

// Heading writes a section heading.
func (d *Doc) Heading(text string) {
	d.ensure(18)
	d.pdf.Ln(2)
	d.pdf.SetFont(family, "B", 12)
	d.pdf.CellFormat(0, 7, text, "", 1, "L", false, 0, "")
	d.pdf.SetFont(family, "", bodySize)
}

// Subheading writes a sub-section heading, kept with the next lines.
func (d *Doc) Subheading(text string) {
	d.ensure(14)
	d.pdf.Ln(1.5)
	d.pdf.SetFont(family, "B", 9.5)
	d.pdf.MultiCell(0, 5, text, "", "L", false)
	d.pdf.SetFont(family, "", bodySize)
}

// Line writes one line of text, shrunk to fit the width.
func (d *Doc) Line(text string) {
	d.fitCell(d.width(), lineH, text, bodySize, "", 1)
}

// Para writes wrapped text.
func (d *Doc) Para(text string) {
	if text == "" {
		return
	}
	d.pdf.SetFont(family, "", bodySize)
	d.pdf.MultiCell(0, lineH, text, "", "L", false)
}

// KV writes a labelled value; long values wrap under the label.
func (d *Doc) KV(label, value string) {
	const lw = 38.0
	d.pdf.SetFont(family, "B", bodySize-0.5)
	d.pdf.CellFormat(lw, lineH, label, "", 0, "L", false, 0, "")
	d.pdf.SetFont(family, "", bodySize)
	d.pdf.MultiCell(0, lineH, value, "", "L", false)
}

// Bullet writes a wrapped list item.
func (d *Doc) Bullet(text string) {
	d.pdf.SetFont(family, "", bodySize)
	d.pdf.CellFormat(4, lineH, "•", "", 0, "L", false, 0, "")
	d.pdf.MultiCell(0, lineH, text, "", "L", false)
}

// Space adds vertical space.
func (d *Doc) Space(mm float64) { d.pdf.Ln(mm) }

// fitCell writes text in one cell, reducing the font size until it fits, so
// that a value is never cut or wrapped into pieces.
func (d *Doc) fitCell(w, h float64, text string, size float64, style string, ln int) {
	d.pdf.SetFont(family, style, size)
	for s := size; s > 4 && d.pdf.GetStringWidth(text) > w-1.5; s -= 0.25 {
		d.pdf.SetFontSize(s)
	}
	d.pdf.CellFormat(w, h, text, "", ln, "L", false, 0, "")
	d.pdf.SetFont(family, "", bodySize)
}

// Column is a table column: a header and a share of the width.
type Column struct {
	Header string
	Weight float64
}

// Table writes rows; the header repeats after each page break. Cells keep
// their whole text on one line (the font shrinks to fit).
func (d *Doc) Table(cols []Column, rows [][]string) {
	total := 0.0
	for _, c := range cols {
		total += c.Weight
	}
	widths := make([]float64, len(cols))
	for i, c := range cols {
		widths[i] = math.Floor(d.width()*c.Weight/total*10) / 10
	}
	const rh = 5.0
	head := func() {
		d.pdf.SetFillColor(235, 235, 235)
		for i, c := range cols {
			d.pdf.SetFont(family, "B", 7.5)
			for s := 7.5; s > 4 && d.pdf.GetStringWidth(c.Header) > widths[i]-1.5; s -= 0.25 {
				d.pdf.SetFontSize(s)
			}
			d.pdf.CellFormat(widths[i], rh, c.Header, "B", 0, "L", true, 0, "")
		}
		d.pdf.Ln(rh)
	}
	d.ensure(3 * rh)
	head()
	for _, row := range rows {
		_, ph := d.pdf.GetPageSize()
		_, bottom := d.pdf.GetAutoPageBreak()
		if d.pdf.GetY()+rh > ph-bottom {
			d.pdf.AddPage()
			head()
		}
		for i := range cols {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			d.fitCell(widths[i], rh, cell, 7.5, "", 0)
		}
		d.pdf.Ln(rh)
	}
	d.pdf.SetFont(family, "", bodySize)
}

// Bytes finishes the document.
func (d *Doc) Bytes() ([]byte, error) {
	var buf bytes.Buffer
	if err := d.pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Clean replaces control characters, which have no glyph, with spaces.
func Clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\n' {
			return ' '
		}
		return r
	}, s)
}
