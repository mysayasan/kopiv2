package services

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
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

// exportNonce makes an export id unguessable.
//
// The id is the only thing between a caller holding SOME export grant and a bundle they did
// not create: the job is looked up by id alone. A timestamp and a counter are enumerable
// inside the six-hour retention window by anybody who can call the route at all, which is a
// cheap thing to fix and an embarrassing one to explain. Falls back to the counter-only
// shape if the system's randomness is unavailable, because refusing to export because a
// suffix could not be generated is the worse failure.
func exportNonce() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "x"
	}
	return hex.EncodeToString(buf)
}

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
		// WHAT THE FILE ACTUALLY CONTAINS, which is not the same as the range that was
		// asked for and until this was nowhere in the bundle.
		//
		// An export is whole stored segments joined without re-encoding, so a request for
		// 14:05-14:40 against fifteen-minute segments produces a file whose first frame is
		// 14:00. The manifest described the REQUEST (`requestedRange`, `coveredSeconds`)
		// and said nothing about the media, so a recipient counting from the start of the
		// file — which is the only thing a recipient can do — was five minutes out on every
		// timestamp they derived. Found by ffprobing the bundle in the W3-3a bench: an
		// eighteen-second clip that was sixty seconds of video.
		//
		// The footage is NOT cut to fit. A stream-copy cut lands on a keyframe rather than
		// on the requested instant and can break the leading GOP, and handing over less
		// than was recorded is a worse answer than handing over more and describing it
		// exactly.
		//
		// StartsAt is the wall-clock moment of the file's first frame; MediaSeconds is how
		// many seconds of video it holds (the sum of the source spans, so gaps between
		// sources are NOT counted — they are listed under `gaps`, and the file jumps across
		// them); RequestedOffsetSeconds is how far into the file the requested range
		// begins.
		StartsAt               int64 `json:"startsAt"`
		EndsAt                 int64 `json:"endsAt"`
		MediaSeconds           int64 `json:"mediaSeconds"`
		RequestedOffsetSeconds int64 `json:"requestedOffsetSeconds"`
	} `json:"output"`

	// Redaction declares that this bundle is a DERIVATIVE, not a pristine copy (W3-6).
	//
	// It exists because redaction and the rest of this file are in direct tension, and the
	// tension is written down a few hundred lines below: "an export must not re-encode,
	// because re-encoding changes every pixel and hands the other side an obvious argument
	// that the footage was processed". Redacting IS re-encoding — it changes pixels on
	// purpose.
	//
	// The answer is not to pretend otherwise. A redacted bundle SAYS it was redacted, in
	// the manifest, in the filename and in VERIFY.txt; it still carries the digests of the
	// SOURCE segments, so a recipient can see exactly what it derives from; and it names
	// the regions that were obscured. The unredacted footage stays on the appliance with
	// its own chain of custody, and can be exported separately by somebody entitled to it.
	//
	// Absent (nil) on an ordinary export, so a bundle that says nothing about redaction is
	// one that was not redacted — rather than one built before the field existed.
	Redaction *ExportRedaction `json:"redaction,omitempty"`
}

// ExportRedaction records what was obscured and why the file is not bit-identical footage.
type ExportRedaction struct {
	// Applied is always true when this block is present; it is spelled out so a reader
	// scanning the JSON cannot mistake the block's presence for a configuration option.
	Applied bool `json:"applied"`
	// Regions are the privacy zones burned in, by name, so the recipient knows what they
	// are NOT being shown rather than merely that something is missing.
	Regions []string `json:"regions"`
	// Faces records the face pass, when one was run (W3-6b). Absent when it was not.
	//
	// It is a SEPARATE block from Regions rather than more names in the same list, because
	// the two make claims of different strength and merging them would quietly promote the
	// weaker one. See ExportFaceRedaction.
	Faces *ExportFaceRedaction `json:"faces,omitempty"`
	// Method is how the pixels were destroyed, in plain words.
	Method string `json:"method"`
	// Note is the sentence for a human reading the manifest.
	Note string `json:"note"`
}

// ExportFaceRedaction records the face pass — and, more importantly, its limits (W3-6b).
//
// A PRIVACY ZONE IS A GUARANTEE AND A FACE PASS IS NOT, and the manifest must never let the
// two be read as the same kind of statement. A zone was named by a human, does not move, and
// is covered. A face pass covers the faces a DETECTOR FOUND — and detectors miss faces in
// profile, at distance, partly occluded or motion-blurred.
//
// So this block reports what was actually done, in counts, and carries a Limitation sentence
// saying plainly that faces may remain. Somebody handing a bundle to a journalist has to know
// that before they hand it over, not when somebody else notices.
type ExportFaceRedaction struct {
	Applied bool `json:"applied"`
	// FramesScanned is every frame the detector looked at; the export fails rather than
	// producing a bundle where some frames went unscanned.
	FramesScanned int `json:"framesScanned"`
	// FacesObscured is the number of DETECTIONS, summed over frames — not a number of
	// people. One person present for a minute is up to a minute of detections, and saying
	// "faces obscured: 900" without this note invites a reader to infer a crowd.
	FacesObscured int `json:"facesObscured"`
	// FramesObscured is how many frames had at least one rectangle filled.
	FramesObscured int `json:"framesObscured"`
	// HoldFrames / MarginPercent are the two safety margins: every detection was also
	// covered for this many frames either side of it, and widened by this much beyond the
	// box the detector returned.
	HoldFrames    int    `json:"holdFrames"`
	MarginPercent int    `json:"marginPercent"`
	Method        string `json:"method"`
	Limitation    string `json:"limitation"`
}

// ExportJob is one export's live state.
type ExportJob struct {
	Id     string       `json:"id"`
	Status ExportStatus `json:"status"`
	// CaseId is set on a CASE bundle and 0 on a single-clip one. The two share this job
	// type because they share the whole build pipeline; which manifest is populated is
	// what tells them apart.
	CaseId    int64           `json:"caseId,omitempty"`
	CameraId  int64           `json:"cameraId"`
	From      int64           `json:"from"`
	To        int64           `json:"to"`
	Reason    string          `json:"reason"`
	CreatedAt int64           `json:"createdAt"`
	Error     string          `json:"error,omitempty"`
	Manifest  *ExportManifest `json:"manifest,omitempty"`
	// CaseManifest is the case bundle's manifest; nil on a single-clip export.
	CaseManifest *CaseManifest `json:"caseManifest,omitempty"`
	// BundlePath is server-side only.
	BundlePath string `json:"-"`
	// GapWarning surfaces the headline fact to the UI without it having to read the
	// manifest: the range is not continuous.
	GapWarning bool `json:"gapWarning"`
}

// ExportRequest is what a caller asks for.
type ExportRequest struct {
	CameraId int64  `json:"cameraId"`
	From     int64  `json:"from"`
	To       int64  `json:"to"`
	Reason   string `json:"reason"`
	// Redact burns the camera's privacy zones into the exported video (W3-6).
	//
	// It is a REQUEST-TIME choice rather than a per-camera setting, because the two
	// bundles answer different questions: an investigator working the incident wants
	// everything that was recorded, and a copy handed outside the organisation must not
	// carry the neighbour's window. The same operator makes both, from the same footage,
	// on different days.
	Redact bool `json:"redact"`
	// BlurFaces obscures the faces a detector finds in the exported video (W3-6b).
	//
	// Independent of Redact: a camera may have no privacy zones and still be handed over
	// with faces hidden, and a bundle may need the neighbour's window covered while every
	// face in it stays visible because the faces are the point.
	//
	// Asking for it on an appliance that cannot do it is an ERROR, not a downgrade. See
	// ErrFaceRedactionUnavailable.
	BlurFaces  bool   `json:"blurFaces"`
	ExporterId int64  `json:"-"`
	Exporter   string `json:"-"`
}

// exportPrivacySource is the slice of IPrivacyService this needs: which regions of a
// camera's view must not leave the building.
type exportPrivacySource interface {
	ExportRegions(ctx context.Context, cameraId int64) ([]PrivacyRegion, error)
}

// IEvidenceExportService creates and tracks evidence bundles.
type IEvidenceExportService interface {
	Create(ctx context.Context, req ExportRequest) (*ExportJob, error)
	Get(id string) (*ExportJob, bool)
	// Preview reports what an export WOULD contain, so the UI can warn about gaps
	// before the operator commits to producing a bundle.
	Preview(ctx context.Context, cameraId, from, to int64) (*ExportManifest, error)
	// CreateCase builds a bundle out of a whole case file — every clip, the notes, and
	// the case's chain of custody. See case_export.go.
	CreateCase(ctx context.Context, req CaseExportRequest) (*ExportJob, error)
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
	// privacy supplies the regions a redacted export must obscure (W3-6). Optional: nil
	// means no zones, and every export behaves exactly as it did before.
	privacy exportPrivacySource
	// faces obscures the faces a detector finds (W3-6b). Optional: nil means the appliance
	// cannot do it, and asking for it is refused rather than silently skipped.
	faces *FaceRedactor

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
	privacy exportPrivacySource,
	faces *FaceRedactor,
) IEvidenceExportService {
	return &evidenceExportService{
		recording:  rec,
		camera:     camera,
		cipher:     cipher,
		privacy:    privacy,
		faces:      faces,
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

	// What the joined file will physically contain. Stated from the SOURCES, not from the
	// request: the file starts where the first source starts, not where the operator asked.
	if len(inRange) > 0 {
		man.Output.StartsAt = inRange[0].StartedAt
		var media int64
		last := int64(0)
		for _, seg := range inRange {
			end := seg.EndedAt
			if end <= 0 {
				end = to
			}
			if end > seg.StartedAt {
				media += end - seg.StartedAt
			}
			if end > last {
				last = end
			}
		}
		man.Output.EndsAt = last
		man.Output.MediaSeconds = media
		if off := from - man.Output.StartsAt; off > 0 {
			man.Output.RequestedOffsetSeconds = off
		}
	}

	for _, seg := range inRange {
		src := SourceSegment{
			SegmentId:  seg.Id,
			File:       filepath.Base(seg.FilePath),
			StartedAt:  seg.StartedAt,
			EndedAt:    seg.EndedAt,
			Sha256:     strings.TrimSpace(seg.Sha256),
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
	// REFUSED HERE, not later. An appliance that cannot obscure faces must say so at the
	// moment somebody asks, while they are still looking at the form — not ten minutes into
	// a job, and above all not by handing back a bundle that did not do it.
	if req.BlurFaces {
		if s.faces == nil {
			return nil, fmt.Errorf("%w: face redaction is not available on this appliance", ErrFaceRedactionUnavailable)
		}
		if err := s.faces.Available(); err != nil {
			return nil, err
		}
	}
	man, segs, err := s.plan(ctx, req.CameraId, req.From, req.To)
	if err != nil {
		return nil, err
	}
	if len(segs) == 0 {
		return nil, fmt.Errorf("there is no footage for that camera in that range")
	}
	// A face pass looks at EVERY frame, so its cost is the length of the clip rather than a
	// fixed overhead. The cap is here rather than inside the redactor so the refusal arrives
	// before any work is done, with the number the operator has to change.
	if req.BlurFaces && man.Output.MediaSeconds > maxFaceRedactionSeconds {
		return nil, fmt.Errorf("hiding faces means looking at every frame, and this range is %d minutes — export at most %d minutes at a time with faces hidden",
			man.Output.MediaSeconds/60, maxFaceRedactionSeconds/60)
	}
	man.Reason = strings.TrimSpace(req.Reason)
	man.ExporterId = req.ExporterId
	man.ExporterName = req.Exporter

	s.mu.Lock()
	s.seq++
	id := fmt.Sprintf("exp-%d-%d-%s", time.Now().UTC().Unix(), s.seq, exportNonce())
	job := &ExportJob{
		Id: id, Status: ExportPending, CameraId: req.CameraId,
		From: req.From, To: req.To, Reason: man.Reason,
		CreatedAt:  time.Now().UTC().Unix(),
		GapWarning: len(man.Gaps) > 0,
	}
	s.jobs[id] = job
	s.mu.Unlock()

	// Detached from the request context: a bundle must not be abandoned half-built
	// because the operator's browser navigated away.
	safego.Go("mymatasan.evidence.export", func() {
		buildCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Minute)
		defer cancel()
		s.build(buildCtx, id, man, segs, req.Redact, req.BlurFaces)
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

func (s *evidenceExportService) build(ctx context.Context, id string, man *ExportManifest, segs []*entities.RecordingSegment, redact, blurFaces bool) {
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

	// A redacted bundle is NAMED as one. The filename is the first thing anybody sees and
	// often the only thing that survives being forwarded, so it carries the fact rather
	// than leaving it to a manifest nobody opens.
	redaction := s.redactionFor(ctx, redact, blurFaces, man.Camera.Id)
	prefix := "camera"
	if redaction != nil {
		prefix = "camera-REDACTED"
	}
	outName := fmt.Sprintf("%s%d_%s-%s.mp4", prefix, man.Camera.Id,
		time.Unix(man.RequestedRange.From, 0).UTC().Format("20060102T150405Z"),
		time.Unix(man.RequestedRange.To, 0).UTC().Format("150405Z"))
	outPath := filepath.Join(dir, outName)
	switch {
	case redaction != nil && redaction.faces:
		// The face pass needs ONE file to scan, so the parts are joined by stream copy
		// first — that join is not the export, it is the input to it.
		joined := filepath.Join(dir, "joined.mp4")
		if err := s.concat(ctx, parts, joined); err != nil {
			fail(err)
			return
		}
		// The zones ride along in the SAME encode: a second pass would degrade the picture
		// twice for no benefit.
		report, err := s.faces.Render(ctx, joined, outPath, dir, redaction.regions)
		_ = os.Remove(joined)
		if err != nil {
			fail(err)
			return
		}
		man.Output.Transcoded = true
		man.Redaction = redaction.manifestWith(&report)
	case redaction != nil:
		if err := s.redact(ctx, parts, outPath, redaction.regions); err != nil {
			fail(err)
			return
		}
		man.Output.Transcoded = true
		man.Redaction = redaction.manifestWith(nil)
	default:
		if err := s.concat(ctx, parts, outPath); err != nil {
			fail(err)
			return
		}
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

// maxFaceRedactionSeconds caps how much footage one face-redacted export will scan.
//
// A face pass reads every frame, so an unbounded range is an unbounded job. Twenty minutes is
// far longer than any clip anybody hands over as evidence and short enough that the operator
// gets an answer the same morning; beyond it they are told the number rather than left
// watching a progress bar that never moves.
const maxFaceRedactionSeconds = int64(20 * 60)

// redactionPlan is what a redacted export needs: the regions, whether faces are being
// obscured, and the names for the manifest.
type redactionPlan struct {
	regions []PrivacyRegion
	names   []string
	faces   bool
}

// manifestWith composes the manifest block once the work is done.
//
// It is composed AFTER the render rather than before, because the face numbers are the whole
// value of the block and they are not known until the pass has run. A block written in
// advance would be a statement of intent formatted as a statement of fact.
func (p *redactionPlan) manifestWith(faces *FaceRedactionReport) *ExportRedaction {
	out := &ExportRedaction{Applied: true, Regions: p.names}
	what := []string{}
	if len(p.names) > 0 {
		what = append(what, "the listed regions")
	}
	if faces != nil {
		what = append(what, "every face the detector found")
		out.Faces = &ExportFaceRedaction{
			Applied:        true,
			FramesScanned:  faces.FramesScanned,
			FacesObscured:  faces.FacesFound,
			FramesObscured: faces.FramesObscured,
			HoldFrames:     faces.HoldFrames,
			MarginPercent:  faces.MarginPercent,
			Method: fmt.Sprintf("every frame was scanned for faces; each detection was filled with solid black, widened by %d%% on every side and held for %d frames either side of the frame it was found in",
				faces.MarginPercent, faces.HoldFrames),
			// THE SENTENCE THIS WHOLE BLOCK EXISTS FOR.
			Limitation: "This is NOT a guarantee that no face is visible in this file. Faces were found by an automatic detector, " +
				"and detectors miss faces that are turned away, distant, partly hidden or blurred by motion. The count above is the " +
				"number of detections, not the number of people. Treat this file as a copy in which the faces that could be found " +
				"have been destroyed — not as a copy that has been checked by a person.",
		}
	}
	out.Method = strings.Join(what, " and ") + " were filled with solid black and the video re-encoded"
	out.Note = "This file is a REDACTED DERIVATIVE of the recorded footage, not a copy of it. " +
		"What is listed above has been destroyed in this file and cannot be recovered from it. " +
		"Every other pixel has also been re-encoded, so this file will not match the digests of the " +
		"source segments listed under `sources` — those digests describe the ORIGINAL footage, which " +
		"remains on the recorder and can be exported separately by somebody entitled to see it."
	return out
}

// redactionFor decides whether this export is redacted, and what it obscures.
//
// A redaction that was ASKED FOR and finds no zones is NOT silently downgraded to an
// ordinary export — but it is also not an error: a camera with nothing to hide has nothing
// to redact, and the bundle simply is not marked as redacted. What must never happen is the
// reverse: a bundle MARKED redacted that had nothing burned into it, which is a false claim
// about what a recipient is being protected from.
func (s *evidenceExportService) redactionFor(ctx context.Context, wantZones, wantFaces bool, cameraId int64) *redactionPlan {
	plan := &redactionPlan{faces: wantFaces && s.faces != nil}
	if wantZones && s.privacy != nil {
		regions, err := s.privacy.ExportRegions(ctx, cameraId)
		if err != nil {
			log.Printf("evidence: cam%d: could not read privacy zones for redaction: %v", cameraId, err)
		}
		for _, r := range regions {
			plan.regions = append(plan.regions, r)
			plan.names = append(plan.names, r.Name)
		}
	}
	// Nothing to do: NOT an error, and NOT a bundle marked redacted. A camera with no zones
	// has nothing to redact, and marking the bundle anyway would be a false claim about what
	// the recipient is being protected from.
	if len(plan.regions) == 0 && !plan.faces {
		return nil
	}
	return plan
}

// redact joins the parts and burns the privacy regions into the result.
//
// THIS IS THE ONE PLACE IN THE PRODUCT THAT DELIBERATELY BREAKS THE RULE STATED ON concat
// BELOW, and it does so loudly. Redaction re-encodes by definition; the answer is not to
// pretend the output is pristine footage but to declare it a derivative — see
// ExportManifest.Redaction and VERIFY.txt.
//
// SOLID BLACK, not blur or pixelation. Blurring is reversible-looking: it invites the
// argument that something could be recovered, and on a low-detail region it sometimes can
// be. A filled box destroys the pixels and looks like what it is. The camera-side mask can
// blur if an operator prefers, because there the original never existed in the first place.
func (s *evidenceExportService) redact(ctx context.Context, parts []string, out string, regions []PrivacyRegion) error {
	ffmpeg := ""
	if s.ffmpegPath != nil {
		ffmpeg = strings.TrimSpace(s.ffmpegPath())
	}
	if ffmpeg == "" {
		return fmt.Errorf("no ffmpeg is configured — set it in Settings before exporting footage")
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

	filter := redactionFilter(regions)
	if filter == "" {
		return errors.New("the privacy zones could not be turned into a redaction")
	}
	cmd := exec.CommandContext(ctx, ffmpeg,
		"-hide_banner", "-loglevel", "error",
		"-f", "concat", "-safe", "0", "-i", filepath.ToSlash(listPath),
		"-vf", filter,
		// A visually lossless re-encode. The pixels change either way; there is no reason
		// for them to also get worse.
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "18",
		"-c:a", "copy",
		"-movflags", "+faststart", "-f", "mp4", "-y", filepath.ToSlash(out))
	procutil.HideWindow(cmd)
	if outBytes, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("redacting the footage failed: %v: %s", err, strings.TrimSpace(string(outBytes)))
	}
	return nil
}

// redactionFilter builds the ffmpeg filter that fills each region.
//
// drawbox over each zone's BOUNDING RECTANGLE. A polygon would need a generated mask image
// per export, and every extra step here is a step that can fail silently on a stream whose
// dimensions are not what the manifest thought — while a bounding box always covers AT
// LEAST the drawn region. Erring towards covering MORE is the only safe direction for a
// privacy control: too much black is a complaint, too little is a disclosure.
func redactionFilter(regions []PrivacyRegion) string {
	parts := make([]string, 0, len(regions))
	for _, region := range regions {
		if len(region.Points) < 3 {
			continue
		}
		minX, minY := region.Points[0][0], region.Points[0][1]
		maxX, maxY := minX, minY
		for _, p := range region.Points[1:] {
			minX, maxX = math.Min(minX, p[0]), math.Max(maxX, p[0])
			minY, maxY = math.Min(minY, p[1]), math.Max(maxY, p[1])
		}
		// Expressed as fractions of the real frame size, so the same zone is correct
		// whatever resolution the camera happens to be recording at — including after
		// somebody changes it, which is the case a pixel rectangle silently gets wrong.
		parts = append(parts, fmt.Sprintf(
			"drawbox=x=iw*%.4f:y=ih*%.4f:w=iw*%.4f:h=ih*%.4f:color=black@1.0:t=fill",
			minX, minY, maxX-minX, maxY-minY))
	}
	return strings.Join(parts, ",")
}

// concat joins the parts. Stream-copy: an export must not re-encode, because re-encoding
// changes every pixel and hands the other side an obvious argument that the footage was
// processed.
//
// A REDACTED export deliberately does the opposite; see redact() above, which declares the
// result a derivative rather than presenting a re-encode as untouched footage.
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

	// SAID FIRST, and in words, when it applies. The manifest carries the same fact in
	// JSON, but this file is the one a person reads — and "you are not being shown
	// everything that was recorded" is not a footnote.
	if man.Redaction != nil && man.Redaction.Applied {
		sb.WriteString("!! THIS IS A REDACTED COPY, NOT THE ORIGINAL FOOTAGE !!\n")
		sb.WriteString("-------------------------------------------------------\n\n")
		sb.WriteString("   Parts of the picture have been permanently blacked out in this file:\n")
		for _, region := range man.Redaction.Regions {
			sb.WriteString("     - " + region + "\n")
		}
		sb.WriteString("\n   They cannot be recovered from this file. The whole video has also been\n")
		sb.WriteString("   re-encoded to do it, so THIS FILE WILL NOT MATCH the digests listed\n")
		sb.WriteString("   under `sources` in manifest.json — those describe the original\n")
		sb.WriteString("   recordings, which are still on the recorder and can be exported\n")
		sb.WriteString("   separately by somebody entitled to see them.\n\n")
		sb.WriteString("   Step 1 below still applies: it proves this file is the one this\n")
		sb.WriteString("   manifest describes, which is a different claim from proving it is\n")
		sb.WriteString("   unaltered footage.\n\n")
	}

	sb.WriteString("1. CHECK THE VIDEO IS THE ONE THIS MANIFEST DESCRIBES\n\n")
	sb.WriteString("   Compute the SHA-256 of the video file and compare it with\n")
	sb.WriteString("   output.sha256 in manifest.json.\n\n")
	sb.WriteString("     Linux/macOS:  shasum -a 256 \"" + mediaName + "\"\n")
	sb.WriteString("     Windows:      certutil -hashfile \"" + mediaName + "\" SHA256\n\n")
	sb.WriteString("   Expected: " + man.Output.Sha256 + "\n\n")

	sb.WriteString("2. WHAT THE VIDEO ACTUALLY COVERS\n\n")
	sb.WriteString("   The footage is NOT cut to the requested range. It is the stored\n")
	sb.WriteString("   recordings covering that range, joined without re-encoding, so the\n")
	sb.WriteString("   file begins and ends on recording boundaries.\n\n")
	sb.WriteString("     Requested:        " + time.Unix(man.RequestedRange.From, 0).UTC().Format(time.RFC3339) +
		"  to  " + time.Unix(man.RequestedRange.To, 0).UTC().Format(time.RFC3339) + "\n")
	sb.WriteString("     The video covers: " + time.Unix(man.Output.StartsAt, 0).UTC().Format(time.RFC3339) +
		"  to  " + time.Unix(man.Output.EndsAt, 0).UTC().Format(time.RFC3339) + "\n")
	sb.WriteString(fmt.Sprintf("     Video length:     %d seconds\n", man.Output.MediaSeconds))
	if man.Output.RequestedOffsetSeconds > 0 {
		sb.WriteString(fmt.Sprintf(
			"\n   THE REQUESTED RANGE BEGINS %d SECOND(S) INTO THE FILE.\n"+
				"   Do not read wall-clock times by counting from the start of the video\n"+
				"   without applying that offset.\n",
			man.Output.RequestedOffsetSeconds))
	}
	sb.WriteString("\n3. UNDERSTAND WHAT THE SOURCE DIGESTS MEAN\n\n")
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
		sb.WriteString("4. THIS EXPORT IS NOT CONTINUOUS\n\n")
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
		sb.WriteString("4. COVERAGE\n\n")
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
