package report

import (
	"bytes"
	"image"
	"image/color"
	"strings"
	"testing"
	"time"
)

func fixedTime() time.Time { return time.Date(2026, 7, 26, 9, 30, 0, 0, time.UTC) }

func TestDocumentOutputsValidPDF(t *testing.T) {
	doc := New(Options{
		Title:       "Fleet Health Report",
		Subtitle:    "3 nodes across 2 sites",
		Period:      "Last 30 days",
		GeneratedAt: fixedTime(),
	})
	doc.H1("Fleet Summary")
	doc.StatTiles([]Tile{
		{Label: "Total", Value: "3", Accent: true},
		{Label: "Online", Value: "2"},
		{Label: "Lost", Value: "1", Danger: true},
	})
	doc.Note("as of report time")
	doc.H1("Nodes")
	rows := make([][]string, 0, 60)
	for i := 0; i < 60; i++ { // enough to force a page break + header repeat
		rows = append(rows, []string{"node-with-a-fairly-long-name-" + itoaTest(i), "Site A", "Online", "2026-07-25 10:00"})
	}
	doc.Table([]Column{
		{Header: "Node", Width: 0},
		{Header: "Site", Width: 30},
		{Header: "Status", Width: 24, Align: "C"},
		{Header: "Last seen", Width: 34},
	}, rows)

	// An embedded image must not panic Output.
	img := image.NewRGBA(image.Rect(0, 0, 120, 80))
	for y := 0; y < 80; y++ {
		for x := 0; x < 120; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 2), 100, 150, 255})
		}
	}
	if err := doc.Image("test-img", img, 100); err != nil {
		t.Fatalf("Image() error = %v", err)
	}

	out, err := doc.Output()
	if err != nil {
		t.Fatalf("Output() error = %v", err)
	}
	if !bytes.HasPrefix(out, []byte("%PDF-")) {
		t.Fatalf("output is not a PDF (prefix %q)", firstBytes(out, 8))
	}
	if len(out) < 1500 {
		t.Fatalf("PDF suspiciously small: %d bytes", len(out))
	}
	// The 60-row table must have spilled onto more than one page.
	if pages := bytes.Count(out, []byte("/Type /Page")); pages < 2 {
		t.Fatalf("expected multi-page output, got %d page object(s)", pages)
	}
}

func TestEmptyStateRenders(t *testing.T) {
	doc := New(Options{Title: "Empty", GeneratedAt: fixedTime()})
	doc.H1("Nothing")
	doc.Empty("No data.")
	out, err := doc.Output()
	if err != nil {
		t.Fatalf("Output() error = %v", err)
	}
	if !bytes.HasPrefix(out, []byte("%PDF-")) {
		t.Fatal("not a PDF")
	}
}

func itoaTest(n int) string {
	if n == 0 {
		return "0"
	}
	var b strings.Builder
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	b.WriteString(digits)
	return b.String()
}

func firstBytes(b []byte, n int) []byte {
	if len(b) < n {
		return b
	}
	return b[:n]
}
