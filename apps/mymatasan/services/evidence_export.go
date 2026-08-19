package services

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	"github.com/mysayasan/kopiv2/infra/atrest"
	"github.com/mysayasan/kopiv2/infra/procutil"
	"github.com/mysayasan/kopiv2/infra/recording"
	"github.com/mysayasan/kopiv2/infra/safego"
)

// Evidence export: hand somebody a defensible copy of a span of footage.
//
// This is the moment the whole product is bought for, and until now it did not exist.
// Playback and download worked per stored segment, so producing "14:05 to 14:40 on
// camera 3" meant an operator downloading several files and stitching them in an
// external tool — which destroys any claim that the footage is unmodified, and produces
// no record that it happened.
//
// A bundle is one video file, a manifest, and a plain-text verification note. The
// manifest is the part that matters: it names the exporter and their stated reason, the
// digest of every source segment and of the output, and — mandatorily — every GAP in the
// range. An export that silently skips missing footage looks continuous, which is
// actively misleading and worse than refusing to export at all.

// ExportStatus is where a job has got to.
type ExportStatus string

const (
	ExportPending ExportStatus = "pending"
	ExportRunning ExportStatus = "running"
	ExportReady   ExportStatus = "ready"
	ExportFailed  ExportStatus = "failed"
)

// exportBundleVersion is stamped into the manifest so a reader knows the shape.
const exportBundleVersion = 1

// exportMaxRangeSeconds bounds one export. A day of one camera is already a large file;
// beyond that an operator wants several exports, each with its own stated reason.
const exportMaxRangeSeconds = int64(24 * 3600)

// exportRetention is how long a finished bundle stays on disk before the janitor removes
// it. Long enough to download, short enough that a bundle of decrypted evidence is not
// left lying beside the encrypted recordings indefinitely.
const exportRetention = 6 * time.Hour

// SourceSegment is one contributing segment in the manifest.
type SourceSegment struct {
	SegmentId int64  `json:"segmentId"`
	File      string `json:"file"`
	StartedAt int64  `json:"startedAt"`
	EndedAt   int64  `json:"endedAt"`
	Sha256    string `json:"sha256"`
	// HashOrigin is "recorded" when the digest was taken at finalize, before the
	// segment was encrypted — the strong claim, meaning the footage has not been
	// altered since it was recorded. It is "computed-at-export" for a segment that has
	// no stored digest (recorded before hashing existed, or adopted after a crash when
	// the file on disk was already encrypted); that digest proves only that the file has
	// not changed since this export, and MUST NOT be read as the stronger claim.
	HashOrigin string `json:"hashOrigin"`
}

// Gap is a span inside the requested range with no footage.
type Gap struct {
	From int64 `json:"from"`
	To   int64 `json:"to"`
	// Reason is why, as far as the system can tell. "no-recording" is the honest
	// default: the footage is absent and the cause was not recorded at the time.
	Reason string `json:"reason"`
}

// ExportManifest is the bundle's evidentiary record.
type ExportManifest struct {
	BundleVersion int    `json:"bundleVersion"`
	App           string `json:"app"`
	AppVersion    string `json:"appVersion"`
	ExportedAt    string `json:"exportedAt"`
	ExporterId    int64  `json:"exporterId"`
	ExporterName  string `json:"exporterName"`
	// Reason is what the operator typed. Required — an evidence export with no stated
	// purpose is the one nobody can account for afterwards.
	Reason string `json:"reason"`

	Camera struct {
		Id       int64  `json:"id"`
		Name     string `json:"name"`
		Location string `json:"location,omitempty"`
		Timezone string `json:"timezone"`
	} `json:"camera"`

	RequestedRange struct {
		From int64 `json:"from"`
		To   int64 `json:"to"`
	} `json:"requestedRange"`
	CoveredRange struct {
		From int64 `json:"from"`
		To   int64 `json:"to"`
	} `json:"coveredRange"`
	CoveredSeconds int64   `json:"coveredSeconds"`
	CoveragePct    float64 `json:"coveragePercent"`

	// Gaps is never omitted. An empty array means "checked, none found"; a missing
	// field would be indistinguishable from "did not look".
	Gaps []Gap `json:"gaps"`

	Sources []SourceSegment `json:"sources"`

	Output struct {
		Filename   string `json:"filename"`
		Sha256     string `json:"sha256"`
		SizeBytes  int64  `json:"sizeBytes"`
		Codec      string `json:"codec"`
		Transcoded bool   `json:"transcoded"`
	} `json:"output"`
}

// ExportJob is one export's live state.
type ExportJob struct {
	Id        string         `json:"id"`
	Status    ExportStatus   `json:"status"`
	CameraId  int64          `json:"cameraId"`
	From      int64          `json:"from"`
	To        int64          `json:"to"`
	Reason    string         `json:"reason"`
	CreatedAt int64          `json:"createdAt"`
	Error     string         `json:"error,omitempty"`
	Manifest  *ExportManifest `json:"manifest,omitempty"`
	// BundlePath is server-side only.
	BundlePath string `json:"-"`
	// GapWarning surfaces the headline fact to the UI without it having to read the
	// manifest: the range is not continuous.
	GapWarning bool `json:"gapWarning"`
}

// ExportRequest is what a caller asks for.
type ExportRequest struct {
	CameraId   int64  `json:"cameraId"`
	From       int64  `json:"from"`
	To         int64  `json:"to"`
	Reason     string `json:"reason"`
	ExporterId int64  `json:"-"`
	Exporter   string `json:"-"`
}

// IEvidenceExportService creates and tracks evidence bundles.
type IEvidenceExportService interface {
	Create(ctx context.Context, req ExportRequest) (*ExportJob, error)
	Get(id string) (*ExportJob, bool)
	// Preview reports what an export WOULD contain, so the UI can warn about gaps
	// before the operator commits to producing a bundle.
	Preview(ctx context.Context, cameraId, from, to int64) (*ExportManifest, error)
}

type evidenceExportService struct {
	recording IRecordingService
	camera    ICameraService
	cipher    *atrest.Cipher
	// ffmpegPath is resolved on every export, not captured at construction. The path
	// lives in runtime settings and the in-app installer rewrites it, so a boot-time
	// copy goes stale the moment an operator installs ffmpeg through the product —
	// and every export after that fails until someone restarts the app.
	ffmpegPath func() string
	workDir    string
	appVersion string

	mu   sync.Mutex
	jobs map[string]*ExportJob
	seq  int64
}

func NewEvidenceExportService(
	rec IRecordingService,
	camera ICameraService,
	cipher *atrest.Cipher,
	ffmpegPath func() string,
	workDir string,
	appVersion string,
) IEvidenceExportService {
	return &evidenceExportService{
		recording:  rec,
		camera:     camera,
		cipher:     cipher,
		ffmpegPath: ffmpegPath,
		workDir:    workDir,
		appVersion: appVersion,
		jobs:       map[string]*ExportJob{},
	}
}

func (s *evidenceExportService) Get(id string) (*ExportJob, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return nil, false
	}
	cp := *j
	return &cp, true
}

// --- planning ---------------------------------------------------------------

// plan resolves the segments overlapping the range and computes the manifest's
// evidentiary facts: what is covered, what is missing, and what each source hashes to.
func (s *evidenceExportService) plan(ctx context.Context, cameraId, from, to int64) (*ExportManifest, []*entities.RecordingSegment, error) {
	if cameraId <= 0 {
		return nil, nil, fmt.Errorf("a camera is required")
	}
	if to <= from {
		return nil, nil, fmt.Errorf("the end of the range must be after its start")
	}
	if to-from > exportMaxRangeSeconds {
		return nil, nil, fmt.Errorf("range too long: export at most %d hours at a time", exportMaxRangeSeconds/3600)
	}

	// Widened backwards for the same reason coverage is: GetSegments filters on
	// StartedAt, so a segment that began before the range and runs into it — usually the
	// one covering the first minutes — would otherwise be missed entirely.
	segs, _, err := s.recording.GetSegments(ctx, coverageQueryLimit, 0, cameraId, 0, from-coverageLookbackSlack, to)
	if err != nil {
		return nil, nil, err
	}

	var inRange []*entities.RecordingSegment
	for _, seg := range segs {
		if seg == nil {
			continue
		}
		end := seg.EndedAt
		if end <= 0 {
			end = to
		}
		if end <= from || seg.StartedAt >= to {
			continue
		}
		inRange = append(inRange, seg)
	}
	sort.Slice(inRange, func(i, j int) bool { return inRange[i].StartedAt < inRange[j].StartedAt })

	man := &ExportManifest{BundleVersion: exportBundleVersion, App: "mymatasan", AppVersion: s.appVersion}
	man.RequestedRange.From = from
	man.RequestedRange.To = to
	man.Camera.Id = cameraId
	man.Camera.Timezone = "UTC"
	if s.camera != nil {
		man.Camera.Name = s.camera.DisplayName(ctx, cameraId)
	}

	// Coverage and gaps come from the same merged-interval maths the coverage report and
	// the continuity monitor use, so an export cannot disagree with the screen that sent
	// the operator to it.
	spans := make([]interval, 0, len(inRange))
	for _, seg := range inRange {
		end := seg.EndedAt
		if end <= 0 {
			end = to
		}
		start := seg.StartedAt
		if start < from {
			start = from
		}
		if end > to {
			end = to
		}
		if end > start {
			spans = append(spans, interval{start: start, end: end})
		}
	}
	merged := mergeIntervals(spans)

	man.Gaps = []Gap{}
	cursor := from
	var covered int64
	for _, sp := range merged {
		if sp.start > cursor {
			man.Gaps = append(man.Gaps, Gap{From: cursor, To: sp.start, Reason: "no-recording"})
		}
		covered += sp.end - sp.start
		cursor = sp.end
	}
	if cursor < to {
		man.Gaps = append(man.Gaps, Gap{From: cursor, To: to, Reason: "no-recording"})
	}
	man.CoveredSeconds = covered
	man.CoveragePct = round2(float64(covered) / float64(to-from) * 100)
	if len(merged) > 0 {
		man.CoveredRange.From = merged[0].start
		man.CoveredRange.To = merged[len(merged)-1].end
	}

	for _, seg := range inRange {
		src := SourceSegment{
			SegmentId: seg.Id,
			File:      filepath.Base(seg.FilePath),
			StartedAt: seg.StartedAt,
			EndedAt:   seg.EndedAt,
			Sha256:    strings.TrimSpace(seg.Sha256),
			HashOrigin: "recorded",
		}
		if src.Sha256 == "" {
			// No stored digest: recorded before hashing existed, or adopted after a
			// crash when the file was already encrypted. It is filled in at build time
			// from the decrypted bytes and labelled honestly — that digest proves only
			// that the file has not changed since this export.
			src.HashOrigin = "computed-at-export"
		}
		man.Sources = append(man.Sources, src)
	}
	return man, inRange, nil
}

func (s *evidenceExportService) Preview(ctx context.Context, cameraId, from, to int64) (*ExportManifest, error) {
	man, _, err := s.plan(ctx, cameraId, from, to)
	return man, err
}

// --- building ---------------------------------------------------------------

func (s *evidenceExportService) Create(ctx context.Context, req ExportRequest) (*ExportJob, error) {
	if strings.TrimSpace(req.Reason) == "" {
		return nil, fmt.Errorf("a reason is required for an evidence export")
	}
	man, segs, err := s.plan(ctx, req.CameraId, req.From, req.To)
	if err != nil {
		return nil, err
	}
	if len(segs) == 0 {
		return nil, fmt.Errorf("there is no footage for that camera in that range")
	}
	man.Reason = strings.TrimSpace(req.Reason)
	man.ExporterId = req.ExporterId
	man.ExporterName = req.Exporter

	s.mu.Lock()
	s.seq++
	id := fmt.Sprintf("exp-%d-%d", time.Now().UTC().Unix(), s.seq)
	job := &ExportJob{
		Id: id, Status: ExportPending, CameraId: req.CameraId,
		From: req.From, To: req.To, Reason: man.Reason,
		CreatedAt: time.Now().UTC().Unix(),
		GapWarning: len(man.Gaps) > 0,
	}
	s.jobs[id] = job
	s.mu.Unlock()

	// Detached from the request context: a bundle must not be abandoned half-built
	// because the operator's browser navigated away.
	safego.Go("mymatasan.evidence.export", func() {
		buildCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Minute)
		defer cancel()
		s.build(buildCtx, id, man, segs)
	})
	cp := *job
	return &cp, nil
}

func (s *evidenceExportService) setStatus(id string, fn func(*ExportJob)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if j, ok := s.jobs[id]; ok {
		fn(j)
	}
}

func (s *evidenceExportService) build(ctx context.Context, id string, man *ExportManifest, segs []*entities.RecordingSegment) {
	s.setStatus(id, func(j *ExportJob) { j.Status = ExportRunning })

	fail := func(err error) {
		s.setStatus(id, func(j *ExportJob) {
			j.Status = ExportFailed
			j.Error = err.Error()
		})
	}

	dir := filepath.Join(s.workDir, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		fail(err)
		return
	}

	// Decrypt each source into the work directory, digesting as we go for any segment
	// that has no recorded hash.
	var parts []string
	for i, seg := range segs {
		part := filepath.Join(dir, fmt.Sprintf("part-%03d.mp4", i))
		sum, err := s.materialize(seg.FilePath, part)
		if err != nil {
			fail(fmt.Errorf("preparing segment %d: %w", seg.Id, err))
			return
		}
		if man.Sources[i].HashOrigin == "computed-at-export" {
			man.Sources[i].Sha256 = sum
		} else if man.Sources[i].Sha256 != sum {
			// The stored digest was taken at finalize on these exact bytes. A mismatch
			// means the footage on disk is not what was recorded, which is precisely
			// what the digest exists to detect — refuse rather than export it silently.
			fail(fmt.Errorf("segment %d failed its integrity check: the file does not match the digest recorded when it was written", seg.Id))
			return
		}
		parts = append(parts, part)
	}

	outName := fmt.Sprintf("camera%d_%s-%s.mp4", man.Camera.Id,
		time.Unix(man.RequestedRange.From, 0).UTC().Format("20060102T150405Z"),
		time.Unix(man.RequestedRange.To, 0).UTC().Format("150405Z"))
	outPath := filepath.Join(dir, outName)
	if err := s.concat(ctx, parts, outPath); err != nil {
		fail(err)
		return
	}

	outSum, err := recording.HashPlaintextFile(outPath)
	if err != nil {
		fail(err)
		return
	}
	fi, err := os.Stat(outPath)
	if err != nil {
		fail(err)
		return
	}
	man.Output.Filename = outName
	man.Output.Sha256 = outSum
	man.Output.SizeBytes = fi.Size()
	man.Output.Codec = segs[0].Codec
	man.ExportedAt = time.Now().UTC().Format(time.RFC3339)

	bundlePath := filepath.Join(dir, strings.TrimSuffix(outName, ".mp4")+".zip")
	if err := writeBundle(bundlePath, outPath, outName, man); err != nil {
		fail(err)
		return
	}
	// The loose media and parts are redundant once the bundle exists, and leaving
	// decrypted footage lying beside the encrypted recordings is exactly what the
	// at-rest encryption is there to prevent.
	for _, p := range append(parts, outPath) {
		_ = os.Remove(p)
	}

	s.setStatus(id, func(j *ExportJob) {
		j.Status = ExportReady
		j.BundlePath = bundlePath
		j.Manifest = man
	})
	s.scheduleCleanup(id, dir)
}

// materialize decrypts one segment into dst and returns the plaintext digest.
func (s *evidenceExportService) materialize(src, dst string) (string, error) {
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	var reader io.Reader = in
	if s.cipher != nil {
		dr, derr := s.cipher.MaybeDecryptingReader(in)
		if derr != nil {
			return "", derr
		}
		reader = dr
	}
	out, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(out, h), reader)
	closeErr := out.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// concat joins the parts. Stream-copy: an export must not re-encode, because re-encoding
// changes every pixel and hands the other side an obvious argument that the footage was
// processed.
func (s *evidenceExportService) concat(ctx context.Context, parts []string, out string) error {
	if len(parts) == 1 {
		return os.Rename(parts[0], out)
	}
	listPath := out + ".list"
	var sb strings.Builder
	for _, p := range parts {
		sb.WriteString("file '" + filepath.ToSlash(p) + "'\n")
	}
	if err := os.WriteFile(listPath, []byte(sb.String()), 0o600); err != nil {
		return err
	}
	defer os.Remove(listPath)

	ffmpeg := ""
	if s.ffmpegPath != nil {
		ffmpeg = strings.TrimSpace(s.ffmpegPath())
	}
	if ffmpeg == "" {
		return fmt.Errorf("no ffmpeg is configured — set it in Settings before exporting footage")
	}
	cmd := exec.CommandContext(ctx, ffmpeg,
		"-hide_banner", "-loglevel", "error",
		"-f", "concat", "-safe", "0", "-i", filepath.ToSlash(listPath),
		"-c", "copy", "-movflags", "+faststart", "-f", "mp4", "-y", filepath.ToSlash(out))
	procutil.HideWindow(cmd)
	if outBytes, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("joining the segments failed: %v: %s", err, strings.TrimSpace(string(outBytes)))
	}
	return nil
}

// writeBundle zips the media, the manifest and the verification note together.
func writeBundle(bundlePath, mediaPath, mediaName string, man *ExportManifest) error {
	zf, err := os.Create(bundlePath)
	if err != nil {
		return err
	}
	defer zf.Close()
	zw := zip.NewWriter(zf)

	media, err := os.Open(mediaPath)
	if err != nil {
		return err
	}
	defer media.Close()
	mw, err := zw.Create(mediaName)
	if err != nil {
		return err
	}
	if _, err := io.Copy(mw, media); err != nil {
		return err
	}

	blob, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return err
	}
	jw, err := zw.Create("manifest.json")
	if err != nil {
		return err
	}
	if _, err := jw.Write(blob); err != nil {
		return err
	}

	vw, err := zw.Create("VERIFY.txt")
	if err != nil {
		return err
	}
	if _, err := vw.Write([]byte(verifyNote(man, mediaName))); err != nil {
		return err
	}
	return zw.Close()
}

// verifyNote is a plain-text explanation of how to check the bundle with standard tools.
// It is written for somebody who did not build this system and may be reading it years
// later — a manifest nobody knows how to verify proves nothing.
func verifyNote(man *ExportManifest, mediaName string) string {
	var sb strings.Builder
	sb.WriteString("HOW TO VERIFY THIS EVIDENCE BUNDLE\n")
	sb.WriteString("==================================\n\n")
	sb.WriteString("This bundle contains:\n")
	sb.WriteString("  " + mediaName + "   the exported video\n")
	sb.WriteString("  manifest.json      what it is, where it came from, and what is missing\n")
	sb.WriteString("  VERIFY.txt         this file\n\n")

	sb.WriteString("1. CHECK THE VIDEO IS THE ONE THIS MANIFEST DESCRIBES\n\n")
	sb.WriteString("   Compute the SHA-256 of the video file and compare it with\n")
	sb.WriteString("   output.sha256 in manifest.json.\n\n")
	sb.WriteString("     Linux/macOS:  shasum -a 256 \"" + mediaName + "\"\n")
	sb.WriteString("     Windows:      certutil -hashfile \"" + mediaName + "\" SHA256\n\n")
	sb.WriteString("   Expected: " + man.Output.Sha256 + "\n\n")

	sb.WriteString("2. UNDERSTAND WHAT THE SOURCE DIGESTS MEAN\n\n")
	sb.WriteString("   Each entry in manifest.json's \"sources\" has a hashOrigin:\n\n")
	sb.WriteString("     \"recorded\"            the digest was taken when the segment was\n")
	sb.WriteString("                           written, before it was stored. The footage has\n")
	sb.WriteString("                           not been altered between recording and export.\n\n")
	sb.WriteString("     \"computed-at-export\"  no digest was taken at recording time (the\n")
	sb.WriteString("                           segment predates that feature, or was recovered\n")
	sb.WriteString("                           after a crash). This digest only shows the file\n")
	sb.WriteString("                           has not changed since THIS export. It is NOT\n")
	sb.WriteString("                           evidence about the period before it.\n\n")

	if len(man.Gaps) > 0 {
		sb.WriteString("3. THIS EXPORT IS NOT CONTINUOUS\n\n")
		sb.WriteString(fmt.Sprintf("   %d period(s) inside the requested range have no footage.\n", len(man.Gaps)))
		sb.WriteString("   The video jumps across them. They are listed in manifest.json\n")
		sb.WriteString("   under \"gaps\", and repeated here:\n\n")
		for _, g := range man.Gaps {
			sb.WriteString(fmt.Sprintf("     %s  to  %s   (%s)\n",
				time.Unix(g.From, 0).UTC().Format(time.RFC3339),
				time.Unix(g.To, 0).UTC().Format(time.RFC3339),
				g.Reason))
		}
		sb.WriteString(fmt.Sprintf("\n   The range is %.1f%% covered.\n\n", man.CoveragePct))
	} else {
		sb.WriteString("3. COVERAGE\n\n")
		sb.WriteString("   The requested range is fully covered: no gaps were found.\n\n")
	}

	sb.WriteString("All times are UTC.\n")
	return sb.String()
}

// scheduleCleanup removes a finished bundle after exportRetention. Decrypted evidence
// must not accumulate on disk beside the encrypted recordings.
func (s *evidenceExportService) scheduleCleanup(id, dir string) {
	safego.Go("mymatasan.evidence.cleanup", func() {
		time.Sleep(exportRetention)
		_ = os.RemoveAll(dir)
		s.mu.Lock()
		delete(s.jobs, id)
		s.mu.Unlock()
	})
}
