package services

import (
	"archive/zip"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	"github.com/mysayasan/kopiv2/infra/recording"
	"github.com/mysayasan/kopiv2/infra/safego"
)

// Exporting a case: the whole investigation as one verifiable file.
//
// The single-clip export (evidence_export.go) answers "give me camera 3 from 14:05 to
// 14:40". A case answers the question the single clip cannot: "give me what happened",
// which is four clips off three cameras, the operator's notes about why each one matters,
// and the record of who assembled it. Handing that over as four separate downloads plus a
// verbal explanation is exactly the handover this feature exists to replace.
//
// IT REUSES THE SINGLE-CLIP MACHINERY WHOLE. Each item goes through the same plan() —
// same segment resolution, same merged-interval coverage maths, same mandatory gap list,
// same refusal on a digest mismatch — and the same materialize/concat. A second
// implementation of "what footage covers this range" would eventually disagree with the
// first, and the two answers would be about evidence.
//
// A MISSING CLIP DOES NOT FAIL THE BUNDLE. If an item's footage is gone — added after the
// fact, or destroyed before the case existed — the bundle is still produced and the clip
// is recorded as missing, with the reason, in the manifest and in VERIFY.txt. Refusing
// would hand back nothing for an investigation that is mostly intact; omitting it silently
// would produce a bundle that looks complete. Saying so is the only honest option, and it
// is the same rule as the gap list one level down.

// caseBundleVersion is stamped into the case manifest so a reader knows the shape. It is
// its own version line, independent of the single-clip bundle's.
const caseBundleVersion = 1

// CaseCustodyEntry is one line of the case's chain of custody, taken from the audit trail.
type CaseCustodyEntry struct {
	At      string `json:"at"`
	Actor   string `json:"actor"`
	Action  string `json:"action"`
	Outcome string `json:"outcome"`
	Detail  string `json:"detail"`
}

// CaseClipEntry is one item's place in the bundle.
type CaseClipEntry struct {
	ItemId   int64  `json:"itemId"`
	Kind     string `json:"kind"`
	CameraId int64  `json:"cameraId"`
	Camera   string `json:"camera"`
	From     int64  `json:"from"`
	To       int64  `json:"to"`
	Label    string `json:"label"`
	Note     string `json:"note"`
	AddedBy  string `json:"addedBy"`
	AddedAt  string `json:"addedAt"`
	// File is the clip's path inside the bundle, empty when there is no clip.
	File string `json:"file,omitempty"`
	// Missing is set when the footage this item names could not be exported, with why.
	Missing string `json:"missing,omitempty"`
	// Evidence is the single-clip manifest for this clip — sources, digests, gaps,
	// coverage. Nil for a note or a missing clip.
	Evidence *ExportManifest `json:"evidence,omitempty"`
}

// CaseManifest is the case bundle's manifest.
type CaseManifest struct {
	BundleVersion int    `json:"bundleVersion"`
	App           string `json:"app"`
	AppVersion    string `json:"appVersion"`
	ExportedAt    string `json:"exportedAt"`
	ExporterId    int64  `json:"exporterId"`
	ExporterName  string `json:"exporterName"`
	Reason        string `json:"reason"`

	Case struct {
		Id       int64  `json:"id"`
		Title    string `json:"title"`
		Summary  string `json:"summary"`
		Status   string `json:"status"`
		Assigned string `json:"assignedTo"`
		OpenedBy string `json:"openedBy"`
		OpenedAt string `json:"openedAt"`
		ClosedBy string `json:"closedBy,omitempty"`
		ClosedAt string `json:"closedAt,omitempty"`
		Outcome  string `json:"outcome,omitempty"`
	} `json:"case"`

	// Clips is never omitted and encodes as [] when empty, for the same reason the
	// single-clip manifest's gap list is: a missing field reads as "did not look".
	Clips []CaseClipEntry `json:"clips"`
	// Notes are the case's note items, which carry no footage.
	Notes []CaseClipEntry `json:"notes"`
	// Custody is the case's own audit trail. Empty when the trail could not be read,
	// which CustodyNote says out loud rather than presenting silence as "nothing
	// happened" — the difference matters in the one document where it matters most.
	Custody     []CaseCustodyEntry `json:"custody"`
	CustodyNote string             `json:"custodyNote,omitempty"`

	Totals struct {
		Items        int   `json:"items"`
		ClipsWritten int   `json:"clipsWritten"`
		ClipsMissing int   `json:"clipsMissing"`
		Notes        int   `json:"notes"`
		SizeBytes    int64 `json:"sizeBytes"`
	} `json:"totals"`
}

// CaseExportRequest is everything the exporter needs. The case service assembles it: this
// service resolves footage, not cases.
type CaseExportRequest struct {
	Case        *entities.CaseFile
	Items       []CaseItemView
	Custody     []CaseCustodyEntry
	CustodyNote string
	Reason      string
	ExporterId  int64
	Exporter    string
}

// CreateCase starts building a case bundle. Like the single-clip export it returns
// immediately and builds in the background — a case of eight clips takes minutes to
// decrypt and join, and a request-scoped job dies the moment the browser navigates away.
func (s *evidenceExportService) CreateCase(ctx context.Context, req CaseExportRequest) (*ExportJob, error) {
	if req.Case == nil || req.Case.Id <= 0 {
		return nil, fmt.Errorf("a case is required")
	}
	if strings.TrimSpace(req.Reason) == "" {
		return nil, fmt.Errorf("a reason is required for an evidence export")
	}
	if len(req.Items) == 0 {
		return nil, fmt.Errorf("this case has nothing in it to export")
	}

	man := &CaseManifest{
		BundleVersion: caseBundleVersion,
		App:           "mymatasan",
		AppVersion:    s.appVersion,
		ExporterId:    req.ExporterId,
		ExporterName:  req.Exporter,
		Reason:        strings.TrimSpace(req.Reason),
		Clips:         []CaseClipEntry{},
		Notes:         []CaseClipEntry{},
		Custody:       req.Custody,
		CustodyNote:   req.CustodyNote,
	}
	if man.Custody == nil {
		man.Custody = []CaseCustodyEntry{}
	}
	man.Case.Id = req.Case.Id
	man.Case.Title = req.Case.Title
	man.Case.Summary = req.Case.Summary
	man.Case.Status = req.Case.Status
	man.Case.Assigned = req.Case.AssignedName
	man.Case.OpenedBy = req.Case.OpenedName
	man.Case.OpenedAt = utcStamp(req.Case.OpenedAt)
	if req.Case.ClosedAt > 0 {
		man.Case.ClosedBy = req.Case.ClosedName
		man.Case.ClosedAt = utcStamp(req.Case.ClosedAt)
		man.Case.Outcome = req.Case.Outcome
	}
	man.Totals.Items = len(req.Items)

	s.mu.Lock()
	s.seq++
	id := fmt.Sprintf("case-%d-%d", time.Now().UTC().Unix(), s.seq)
	job := &ExportJob{
		Id: id, Status: ExportPending, CaseId: req.Case.Id,
		Reason: man.Reason, CreatedAt: time.Now().UTC().Unix(),
	}
	s.jobs[id] = job
	s.mu.Unlock()

	items := append([]CaseItemView(nil), req.Items...)
	safego.Go("mymatasan.evidence.case-export", func() {
		buildCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Minute)
		defer cancel()
		s.buildCase(buildCtx, id, man, items)
	})
	cp := *job
	return &cp, nil
}

func (s *evidenceExportService) buildCase(ctx context.Context, id string, man *CaseManifest, items []CaseItemView) {
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

	type builtClip struct {
		entry CaseClipEntry
		path  string
	}
	var clips []builtClip

	for i, item := range items {
		if item.CaseItem == nil {
			continue
		}
		entry := CaseClipEntry{
			ItemId: item.Id, Kind: item.Kind, CameraId: item.CameraId, Camera: item.CameraName,
			From: item.StartedAt, To: item.EndedAt, Label: item.Label, Note: item.Note,
			AddedBy: item.AddedName, AddedAt: utcStamp(item.AddedAt),
		}
		if !item.CaseItem.HoldsFootage() {
			man.Notes = append(man.Notes, entry)
			man.Totals.Notes++
			continue
		}

		clipMan, segs, err := s.plan(ctx, item.CameraId, item.StartedAt, item.EndedAt)
		if err != nil || len(segs) == 0 {
			entry.Missing = "no footage for this camera in this range"
			if err != nil {
				entry.Missing = err.Error()
			}
			man.Clips = append(man.Clips, entry)
			man.Totals.ClipsMissing++
			continue
		}

		clipDir := filepath.Join(dir, fmt.Sprintf("item-%03d", i+1))
		if err := os.MkdirAll(clipDir, 0o700); err != nil {
			fail(err)
			return
		}
		var parts []string
		mismatch := ""
		for n, seg := range segs {
			part := filepath.Join(clipDir, fmt.Sprintf("part-%03d.mp4", n))
			sum, err := s.materialize(seg.FilePath, part)
			if err != nil {
				// One unreadable source is this clip's problem, not the bundle's. It is
				// recorded as missing with the reason, and the other clips still ship.
				mismatch = fmt.Sprintf("segment %d could not be read: %v", seg.Id, err)
				break
			}
			if clipMan.Sources[n].HashOrigin == "computed-at-export" {
				clipMan.Sources[n].Sha256 = sum
			} else if clipMan.Sources[n].Sha256 != sum {
				// Same refusal as the single-clip export: footage that does not match the
				// digest taken when it was written is not exported as if it did.
				mismatch = fmt.Sprintf("segment %d failed its integrity check: the file does not match the digest recorded when it was written", seg.Id)
				break
			}
			parts = append(parts, part)
		}
		if mismatch != "" {
			entry.Missing = mismatch
			man.Clips = append(man.Clips, entry)
			man.Totals.ClipsMissing++
			continue
		}

		outName := fmt.Sprintf("%03d_camera%d_%s-%s.mp4", i+1, item.CameraId,
			time.Unix(item.StartedAt, 0).UTC().Format("20060102T150405Z"),
			time.Unix(item.EndedAt, 0).UTC().Format("150405Z"))
		outPath := filepath.Join(clipDir, outName)
		if err := s.concat(ctx, parts, outPath); err != nil {
			// A join failure is an appliance problem, not an evidence problem — ffmpeg
			// missing fails every clip identically, so failing the job says so once
			// instead of producing a bundle of "missing" entries that blame the footage.
			fail(err)
			return
		}
		sum, err := recording.HashPlaintextFile(outPath)
		if err != nil {
			fail(err)
			return
		}
		fi, err := os.Stat(outPath)
		if err != nil {
			fail(err)
			return
		}
		clipMan.Reason = man.Reason
		clipMan.ExporterId = man.ExporterId
		clipMan.ExporterName = man.ExporterName
		clipMan.ExportedAt = time.Now().UTC().Format(time.RFC3339)
		clipMan.Output.Filename = outName
		clipMan.Output.Sha256 = sum
		clipMan.Output.SizeBytes = fi.Size()
		clipMan.Output.Codec = segs[0].Codec

		entry.File = "clips/" + outName
		entry.Evidence = clipMan
		man.Clips = append(man.Clips, entry)
		man.Totals.ClipsWritten++
		man.Totals.SizeBytes += fi.Size()
		clips = append(clips, builtClip{entry: entry, path: outPath})
	}

	man.ExportedAt = time.Now().UTC().Format(time.RFC3339)
	bundleName := fmt.Sprintf("case%d_%s.zip", man.Case.Id, time.Now().UTC().Format("20060102T150405Z"))
	bundlePath := filepath.Join(dir, bundleName)

	files := make(map[string]string, len(clips))
	for _, c := range clips {
		files[c.entry.File] = c.path
	}
	if err := writeCaseBundle(bundlePath, files, man); err != nil {
		fail(err)
		return
	}
	// The loose decrypted clips are redundant the moment the bundle exists, and leaving
	// them beside the encrypted recordings is what the at-rest encryption is there to
	// prevent.
	for _, c := range clips {
		_ = os.RemoveAll(filepath.Dir(c.path))
	}

	s.setStatus(id, func(j *ExportJob) {
		j.Status = ExportReady
		j.BundlePath = bundlePath
		j.CaseManifest = man
	})
	s.scheduleCleanup(id, dir)
}

// writeCaseBundle zips the clips, the manifest, the chain of custody and the note.
func writeCaseBundle(bundlePath string, files map[string]string, man *CaseManifest) error {
	zf, err := os.Create(bundlePath)
	if err != nil {
		return err
	}
	defer zf.Close()
	zw := zip.NewWriter(zf)

	// Deterministic order: a bundle that zips its clips in map order is a bundle whose
	// byte content differs between two exports of the same case for no reason.
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sortStrings(names)
	for _, name := range names {
		src, err := os.Open(files[name])
		if err != nil {
			return err
		}
		w, err := zw.Create(name)
		if err != nil {
			src.Close()
			return err
		}
		_, copyErr := io.Copy(w, src)
		src.Close()
		if copyErr != nil {
			return copyErr
		}
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

	cw, err := zw.Create("chain-of-custody.csv")
	if err != nil {
		return err
	}
	// CSV as well as JSON because the chain of custody is the part somebody opens in a
	// spreadsheet, prints, and attaches to a report.
	cs := csv.NewWriter(cw)
	_ = cs.Write([]string{"time", "actor", "action", "outcome", "detail"})
	for _, e := range man.Custody {
		_ = cs.Write([]string{e.At, e.Actor, e.Action, e.Outcome, e.Detail})
	}
	cs.Flush()
	if err := cs.Error(); err != nil {
		return err
	}

	vw, err := zw.Create("VERIFY.txt")
	if err != nil {
		return err
	}
	if _, err := vw.Write([]byte(caseVerifyNote(man))); err != nil {
		return err
	}
	return zw.Close()
}

// caseVerifyNote explains the bundle to somebody who did not build this system and may be
// reading it years later. Same purpose as the single-clip note, and it must be equally
// blunt about what the bundle does NOT contain.
func caseVerifyNote(man *CaseManifest) string {
	var sb strings.Builder
	sb.WriteString("HOW TO VERIFY THIS CASE BUNDLE\n")
	sb.WriteString("==============================\n\n")
	sb.WriteString("Case " + strconv.FormatInt(man.Case.Id, 10) + ": " + man.Case.Title + "\n")
	sb.WriteString("Exported " + man.ExportedAt + " by " + man.ExporterName + "\n")
	sb.WriteString("Reason: " + man.Reason + "\n\n")

	sb.WriteString("This bundle contains:\n")
	sb.WriteString("  clips/                the exported video, one file per piece of evidence\n")
	sb.WriteString("  manifest.json         what each clip is, where it came from, what is missing\n")
	sb.WriteString("  chain-of-custody.csv  every recorded action on this case, in order\n")
	sb.WriteString("  VERIFY.txt            this file\n\n")

	sb.WriteString("1. CHECK EACH CLIP IS THE ONE THE MANIFEST DESCRIBES\n\n")
	sb.WriteString("   Every clip in manifest.json carries evidence.output.sha256.\n")
	sb.WriteString("   Compute the SHA-256 of the file and compare.\n\n")
	sb.WriteString("     Linux/macOS:  shasum -a 256 clips/*.mp4\n")
	sb.WriteString("     Windows:      certutil -hashfile <file> SHA256\n\n")
	for _, clip := range man.Clips {
		if clip.File == "" || clip.Evidence == nil {
			continue
		}
		sb.WriteString("     " + clip.File + "\n       " + clip.Evidence.Output.Sha256 + "\n")
	}
	sb.WriteString("\n")

	sb.WriteString("2. WHAT THE SOURCE DIGESTS MEAN\n\n")
	sb.WriteString("   Each clip's evidence.sources entries carry a hashOrigin:\n\n")
	sb.WriteString("     \"recorded\"            the digest was taken when the segment was\n")
	sb.WriteString("                           written. The footage has not been altered\n")
	sb.WriteString("                           between recording and export.\n\n")
	sb.WriteString("     \"computed-at-export\"  no digest was taken at recording time. This\n")
	sb.WriteString("                           digest only shows the file has not changed\n")
	sb.WriteString("                           since THIS export.\n\n")

	if man.Totals.ClipsMissing > 0 {
		sb.WriteString("3. THIS BUNDLE IS INCOMPLETE\n\n")
		sb.WriteString(fmt.Sprintf("   %d of the %d pieces of evidence in this case could not be\n",
			man.Totals.ClipsMissing, man.Totals.ClipsMissing+man.Totals.ClipsWritten))
		sb.WriteString("   exported. They are listed in manifest.json with a \"missing\"\n")
		sb.WriteString("   reason, and repeated here:\n\n")
		for _, clip := range man.Clips {
			if clip.Missing == "" {
				continue
			}
			sb.WriteString(fmt.Sprintf("     camera %d  %s to %s\n       %s\n",
				clip.CameraId, utcStamp(clip.From), utcStamp(clip.To), clip.Missing))
		}
		sb.WriteString("\n")
	} else {
		sb.WriteString("3. COMPLETENESS\n\n")
		sb.WriteString("   Every piece of footage in this case was exported.\n\n")
	}

	sb.WriteString("4. GAPS INSIDE A CLIP\n\n")
	sb.WriteString("   A clip whose range has periods with no recording jumps across them.\n")
	sb.WriteString("   Those periods are listed in that clip's evidence.gaps. A clip with an\n")
	sb.WriteString("   empty gap list is continuous.\n\n")

	if man.CustodyNote != "" {
		sb.WriteString("5. CHAIN OF CUSTODY\n\n   " + man.CustodyNote + "\n\n")
	}

	sb.WriteString("All times are UTC.\n")
	return sb.String()
}

func utcStamp(unix int64) string {
	if unix <= 0 {
		return ""
	}
	return time.Unix(unix, 0).UTC().Format(time.RFC3339)
}

// sortStrings is a tiny local sort so this file does not pull in sort just for one call
// site; the slices are a handful of clip names.
func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}
