package apis

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/domain/entities"
	"github.com/mysayasan/kopiv2/domain/shared/services"
)

func TestParseDownloadIDs(t *testing.T) {
	ids, err := parseDownloadIDs("2, 1,3")
	if err != nil {
		t.Fatalf("parse ids failed: %v", err)
	}
	if len(ids) != 3 || ids[0] != 2 || ids[1] != 1 || ids[2] != 3 {
		t.Fatalf("unexpected ids: %+v", ids)
	}
}

func TestParseExpirationSupportsCountdownUnits(t *testing.T) {
	now := time.Date(2026, 6, 7, 8, 0, 0, 0, time.UTC)

	expiredAt, err := parseExpiration("0", "0", "", now)
	if err != nil {
		t.Fatalf("zero-valued optional fields should mean no expiry: %v", err)
	}
	if expiredAt != 0 {
		t.Fatalf("expected no expiry, got %d", expiredAt)
	}

	expiredAt, err = parseExpiration("", "2", "months", now)
	if err != nil {
		t.Fatalf("parse month countdown failed: %v", err)
	}
	expected := time.Date(2026, 8, 7, 8, 0, 0, 0, time.UTC).Unix()
	if expiredAt != expected {
		t.Fatalf("unexpected month expiry: got %d want %d", expiredAt, expected)
	}

	expiredAt, err = parseExpiration("", "3", "week", now)
	if err != nil {
		t.Fatalf("parse week countdown failed: %v", err)
	}
	expected = time.Date(2026, 6, 28, 8, 0, 0, 0, time.UTC).Unix()
	if expiredAt != expected {
		t.Fatalf("unexpected week expiry: got %d want %d", expiredAt, expected)
	}

	expiredAt, err = parseExpiration("0", "2", "month", now)
	if err != nil {
		t.Fatalf("zero expiredAt should not conflict with countdown: %v", err)
	}
	expected = time.Date(2026, 8, 7, 8, 0, 0, 0, time.UTC).Unix()
	if expiredAt != expected {
		t.Fatalf("unexpected zero-expiredAt countdown expiry: got %d want %d", expiredAt, expected)
	}
}

func TestParseExpirationRejectsAmbiguousExpiryInputs(t *testing.T) {
	now := time.Date(2026, 6, 7, 8, 0, 0, 0, time.UTC)

	if _, err := parseExpiration("1780828800", "2", "month", now); err == nil {
		t.Fatal("expected expiredAt plus countdown to be rejected")
	}
	if _, err := parseExpiration("", "2", "", now); err == nil {
		t.Fatal("expected missing expiresInUnit to be rejected")
	}
	if _, err := parseExpiration("", "", "month", now); err == nil {
		t.Fatal("expected missing expiresIn to be rejected")
	}
	if _, err := parseExpiration("", "0", "month", now); err == nil {
		t.Fatal("expected non-positive expiresIn to be rejected")
	}
}

func TestWriteSingleDownloadCanRenderInline(t *testing.T) {
	rec := httptest.NewRecorder()

	writeSingleDownload(rec, &services.FileStorageDownload{
		Model:    entities.FileStorage{Id: 1, Guid: "guid-1"},
		Filename: "receipt.pdf",
		MimeType: "application/pdf",
		Content:  []byte("pdf"),
	}, true)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Disposition"); got != `inline; filename="receipt.pdf"` {
		t.Fatalf("unexpected content disposition: %q", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/pdf" {
		t.Fatalf("unexpected content type: %q", got)
	}
}

func TestWriteSingleDownloadDefaultsToAttachment(t *testing.T) {
	rec := httptest.NewRecorder()

	writeSingleDownload(rec, &services.FileStorageDownload{
		Model:    entities.FileStorage{Id: 1, Guid: "guid-1"},
		Filename: "receipt.pdf",
		MimeType: "application/pdf",
		Content:  []byte("pdf"),
	}, false)

	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="receipt.pdf"` {
		t.Fatalf("unexpected content disposition: %q", got)
	}
}

func TestBuildDownloadZipUsesSafeUniqueNames(t *testing.T) {
	content, err := buildDownloadZip([]*services.FileStorageDownload{
		{Model: entities.FileStorage{Id: 1, Guid: "guid-1"}, Filename: "report?.txt", Content: []byte("one")},
		{Model: entities.FileStorage{Id: 2, Guid: "guid-2"}, Filename: "report?.txt", Content: []byte("two")},
	})
	if err != nil {
		t.Fatalf("build zip failed: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		t.Fatalf("read zip failed: %v", err)
	}
	if len(zr.File) != 2 {
		t.Fatalf("expected two zip files, got %d", len(zr.File))
	}
	if zr.File[0].Name != "report_.txt" || zr.File[1].Name != "report__2.txt" {
		t.Fatalf("unexpected zip names: %q %q", zr.File[0].Name, zr.File[1].Name)
	}
}
