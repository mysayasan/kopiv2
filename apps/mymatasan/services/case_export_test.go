package services

import (
	"archive/zip"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
)

// buildCaseBundle drives a case export to completion and returns the finished job.
func buildCaseBundle(t *testing.T, svc *evidenceExportService, req CaseExportRequest) *ExportJob {
	t.Helper()
	job, err := svc.CreateCase(context.Background(), req)
	if err != nil {
		t.Fatalf("start export: %v", err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		got, ok := svc.Get(job.Id)
		if !ok {
			t.Fatal("the export job vanished")
		}
		switch got.Status {
		case ExportReady:
			return got
		case ExportFailed:
			t.Fatalf("export failed: %s", got.Error)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the export never finished")
	return nil
}

func readBundle(t *testing.T, path string) map[string][]byte {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open bundle: %v", err)
	}
	defer zr.Close()
	out := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		blob, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read %s: %v", f.Name, err)
		}
		out[f.Name] = blob
	}
	return out
}

// THE POINT OF THIS TEST: one missing clip must not sink the bundle, and it must not
// disappear from it either. An investigation that is mostly intact still has to be
// handed over, and a bundle that silently omits the part that is gone looks complete.
func TestACaseBundleShipsWhatItHasAndSaysWhatIsMissing(t *testing.T) {
	dir := t.TempDir()
	media := filepath.Join(dir, "seg1.mp4")
	if err := os.WriteFile(media, []byte("pretend this is video"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	now := int64(1_700_000_000)
	segments := &holdSegmentRepo{rows: []*entities.RecordingSegment{
		{Id: 1, CameraId: 3, StartedAt: now - 900, EndedAt: now, FilePath: media, Codec: "h264"},
	}}
	rec := &recordingService{segments: segments, configs: &holdConfigRepo{}}
	svc := &evidenceExportService{
		recording:  rec,
		workDir:    filepath.Join(dir, "exports"),
		appVersion: "test",
		jobs:       map[string]*ExportJob{},
		ffmpegPath: func() string { return "" },
	}

	items := []CaseItemView{
		{CaseItem: &entities.CaseItem{
			Id: 1, Kind: entities.CaseItemFootage, CameraId: 3,
			StartedAt: now - 800, EndedAt: now - 100, Label: "person",
			Note: "the jacket", AddedName: "sam", AddedAt: now,
		}, CameraName: "Loading bay"},
		{CaseItem: &entities.CaseItem{
			Id: 2, Kind: entities.CaseItemFootage, CameraId: 4,
			StartedAt: now - 50_000, EndedAt: now - 49_000, AddedName: "sam", AddedAt: now,
		}, CameraName: "Car park"},
		{CaseItem: &entities.CaseItem{
			Id: 3, Kind: entities.CaseItemNote, Note: "same person as item 1",
			StartedAt: now, AddedName: "sam", AddedAt: now,
		}},
	}

	job := buildCaseBundle(t, svc, CaseExportRequest{
		Case: &entities.CaseFile{
			Id: 12, Title: "Loading bay theft", Status: entities.CaseStatusOpen,
			OpenedName: "sam", OpenedAt: now - 3600,
		},
		Items:      items,
		Reason:     "handed to police",
		ExporterId: 7,
		Exporter:   "sam",
		Custody: []CaseCustodyEntry{
			{At: "2026-08-24T10:00:00Z", Actor: "sam", Action: "case.create", Outcome: "success", Detail: "opened case 12"},
		},
	})

	man := job.CaseManifest
	if man == nil {
		t.Fatal("a finished case export must carry its manifest")
	}
	if man.Totals.ClipsWritten != 1 || man.Totals.ClipsMissing != 1 || man.Totals.Notes != 1 {
		t.Fatalf("totals wrong: %+v", man.Totals)
	}
	var missing, written *CaseClipEntry
	for i := range man.Clips {
		if man.Clips[i].Missing != "" {
			missing = &man.Clips[i]
		} else {
			written = &man.Clips[i]
		}
	}
	if missing == nil || missing.CameraId != 4 {
		t.Fatalf("the clip with no footage must be listed as missing: %+v", man.Clips)
	}
	if written == nil || written.Evidence == nil || written.Evidence.Output.Sha256 == "" {
		t.Fatalf("the exported clip must carry its digest: %+v", written)
	}
	// Gaps encode as [] rather than being omitted, exactly as the single-clip manifest
	// does: a missing field reads as "did not look".
	if written.Evidence.Gaps == nil {
		t.Fatal("a clip's gap list must never be omitted")
	}
	// WHAT THE FILE HOLDS, not what was asked for. The clip is the whole 900-second
	// segment; the bookmark is 700 seconds starting 100 seconds in. A manifest that
	// described only the bookmark left a recipient counting wall-clock times from the
	// wrong first frame - the defect the W3-3a bench found by ffprobing the bundle.
	out := written.Evidence.Output
	if out.StartsAt != now-900 || out.EndsAt != now {
		t.Fatalf("the manifest must say when the video starts and ends: %+v", out)
	}
	if out.MediaSeconds != 900 {
		t.Fatalf("the manifest must say how many seconds of video the file holds, got %d", out.MediaSeconds)
	}
	if out.RequestedOffsetSeconds != 100 {
		t.Fatalf("the manifest must say how far into the file the bookmark begins, got %d",
			out.RequestedOffsetSeconds)
	}

	files := readBundle(t, job.BundlePath)
	if _, ok := files[written.File]; !ok {
		t.Fatalf("the clip named in the manifest is not in the bundle: %s (have %v)", written.File, keysOf(files))
	}
	for _, want := range []string{"manifest.json", "VERIFY.txt", "chain-of-custody.csv"} {
		if _, ok := files[want]; !ok {
			t.Fatalf("the bundle is missing %s (have %v)", want, keysOf(files))
		}
	}
	// The verification note must SAY the bundle is incomplete. The manifest is for a
	// machine; this file is what a person reads, and the incompleteness is the thing they
	// most need to be told.
	note := string(files["VERIFY.txt"])
	if !strings.Contains(note, "INCOMPLETE") {
		t.Fatalf("VERIFY.txt must say the bundle is incomplete:\n%s", note)
	}
	// The person reading VERIFY.txt is the one handing the bundle over, and the offset
	// between a clip's first frame and the moment that was bookmarked has to be in there
	// in words, not only as a number in the JSON.
	if !strings.Contains(note, "the bookmark begins 100s in") {
		t.Fatalf("VERIFY.txt must state where the bookmark begins inside the clip:\n%s", note)
	}
	custody := string(files["chain-of-custody.csv"])
	if !strings.Contains(custody, "case.create") || !strings.Contains(custody, "opened case 12") {
		t.Fatalf("the chain of custody did not make it into the bundle:\n%s", custody)
	}

	// The manifest on disk must be the manifest the job reports.
	var onDisk CaseManifest
	if err := json.Unmarshal(files["manifest.json"], &onDisk); err != nil {
		t.Fatalf("manifest.json is not valid JSON: %v", err)
	}
	if onDisk.Case.Id != 12 || onDisk.Reason != "handed to police" || onDisk.ExporterName != "sam" {
		t.Fatalf("manifest does not name the case, the reason and the exporter: %+v", onDisk.Case)
	}

	// Decrypted footage must not be left lying beside the bundle.
	entries, err := os.ReadDir(filepath.Dir(job.BundlePath))
	if err != nil {
		t.Fatalf("read work dir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != filepath.Base(job.BundlePath) {
			t.Fatalf("the export left %s behind beside the bundle", e.Name())
		}
	}
}

func TestACaseExportRefusesWithoutAReasonOrContents(t *testing.T) {
	svc := &evidenceExportService{jobs: map[string]*ExportJob{}, workDir: t.TempDir()}
	row := &entities.CaseFile{Id: 1, Title: "x"}
	item := []CaseItemView{{CaseItem: &entities.CaseItem{Id: 1, Kind: entities.CaseItemNote, Note: "n"}}}
	if _, err := svc.CreateCase(context.Background(), CaseExportRequest{Case: row, Items: item}); err == nil {
		t.Fatal("an export with no stated reason must be refused")
	}
	if _, err := svc.CreateCase(context.Background(), CaseExportRequest{Case: row, Reason: "why"}); err == nil {
		t.Fatal("an export of an empty case must be refused")
	}
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}
