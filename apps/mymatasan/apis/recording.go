package apis

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/infra/atrest"
	"github.com/mysayasan/kopiv2/infra/onvif"
	"github.com/mysayasan/kopiv2/infra/recording"
	"github.com/mysayasan/kopiv2/infra/vision"
)

type recordingApi struct {
	serv     services.IRecordingService
	recorder *recording.Manager
	camera   services.ICameraService
	settings services.IRuntimeSettingsService
	// cipher decrypts segments on the playback path. The recorder's own cipher now comes
	// from recorderCfg.
	cipher *atrest.Cipher
	vision services.IVisionService
	// recorderCfg is the one builder that turns a stored recording config into a runnable
	// RecorderConfig. This handler used to hand-roll that struct, duplicating app wiring —
	// which is how ShredPasses got silently dropped and secure shred degraded to a plain
	// unlink for anyone who saved a recording setting.
	recorderCfg *services.RecorderConfigBuilder
	// audit records evidence handling: who viewed, downloaded, deleted or purged footage.
	// Nil is tolerated (Auditor.Record no-ops) so a partially-wired test handler still works.
	audit *Auditor
}

// NewRecordingApi registers recording routes under /recording.
func NewRecordingApi(router *mux.Router, serv services.IRecordingService, recorder *recording.Manager, camera services.ICameraService, settings services.IRuntimeSettingsService, cipher *atrest.Cipher, vision services.IVisionService, recorderCfg *services.RecorderConfigBuilder, audit *Auditor) {
	h := &recordingApi{serv: serv, recorder: recorder, camera: camera, settings: settings, cipher: cipher, vision: vision, recorderCfg: recorderCfg, audit: audit}
	g := router.PathPrefix("/recording").Subrouter()

	g.HandleFunc("/segments", h.listSegments).Methods("GET")
	g.HandleFunc("/segments/purge", h.purgeExpired).Methods("POST")
	g.HandleFunc("/purge-camera", h.purgeCameraNow).Methods("POST")
	g.HandleFunc("/segments/{id}", h.deleteSegment).Methods("DELETE")
	g.HandleFunc("/segments/{id}/download", h.downloadSegment).Methods("GET")
	g.HandleFunc("/segments/{id}/frame", h.segmentFrame).Methods("GET")
	g.HandleFunc("/config", h.listConfigs).Methods("GET")
	g.HandleFunc("/config", h.saveConfig).Methods("PUT")
	g.HandleFunc("/config/{cameraId}", h.getConfig).Methods("GET")
	g.HandleFunc("/status", h.recorderStatus).Methods("GET")
	g.HandleFunc("/storage/status", h.storageStatus).Methods("GET")
	g.HandleFunc("/streams/{cameraId}", h.listCameraStreams).Methods("GET")
	g.HandleFunc("/streams/{cameraId}/live", h.setLiveStream).Methods("POST")
}

func (a *recordingApi) listSegments(w http.ResponseWriter, r *http.Request) {
	limit, offset := readPaging(r)
	cameraId := parseInt64Query(r, "cameraId")
	alertId := parseInt64Query(r, "alertId")
	startedAfter := parseInt64Query(r, "startedAfter")
	startedBefore := parseInt64Query(r, "startedBefore")

	segs, total, err := a.serv.GetSegments(r.Context(), limit, offset, cameraId, alertId, startedAfter, startedBefore)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{
		"items": segs,
		"total": total,
	}, "succeed")
}

// purgeExpired runs the retention purge on demand: deletes recorded segments that
// are already older than each camera's configured retention (the same safe sweep
// the auto-job runs). It never touches in-retention footage. Returns the count.
func (a *recordingApi) purgeExpired(w http.ResponseWriter, r *http.Request) {
	deleted, err := a.serv.PurgeOldSegments(r.Context())
	if err != nil {
		a.audit.Failure(r, services.ActionRecordingPurge, services.TargetRecording, "expired", err.Error(), nil)
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	a.audit.Success(r, services.ActionRecordingPurge, services.TargetRecording, "expired",
		fmt.Sprintf("retention purge removed %d segment(s)", deleted), map[string]any{"deleted": deleted})
	controllers.SendResult(w, map[string]int{"deleted": deleted}, "succeed")
}

// purgeCameraNow deletes ALL footage AND AI-event snapshots for one camera, ignoring
// expiry — the per-camera "Purge now" action. Body/query: cameraId. Footage removal is
// authoritative (its error fails the request); snapshot removal is best-effort so a
// snapshot hiccup can't leave the footage half-purged.
func (a *recordingApi) purgeCameraNow(w http.ResponseWriter, r *http.Request) {
	cameraId := parseInt64Query(r, "cameraId")
	if cameraId <= 0 {
		var body struct {
			CameraId int64 `json:"cameraId"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		cameraId = body.CameraId
	}
	if cameraId <= 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "cameraId is required")
		return
	}
	segments, err := a.serv.PurgeAllForCamera(r.Context(), cameraId)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	snapshots := 0
	if a.vision != nil {
		if n, verr := a.vision.PurgeAlertsForCamera(r.Context(), cameraId); verr != nil {
			log.Printf("purge-camera %d: snapshot purge warning: %v", cameraId, verr)
		} else {
			snapshots = n
		}
	}
	a.audit.Success(r, services.ActionRecordingPurge, services.TargetCamera, strconv.FormatInt(cameraId, 10),
		fmt.Sprintf("purged all footage for camera %d", cameraId),
		map[string]any{"segments": segments, "snapshots": snapshots})
	controllers.SendResult(w, map[string]int{"segments": segments, "snapshots": snapshots}, "succeed")
}

func (a *recordingApi) deleteSegment(w http.ResponseWriter, r *http.Request) {
	id, ok := readRecordingID(w, r)
	if !ok {
		return
	}
	// Read the row BEFORE deleting it. Afterwards the camera and time range are gone, and
	// "recording 412 was deleted" is not an answer to "what footage did we lose".
	seg, _ := a.serv.GetSegmentById(r.Context(), id)
	meta := map[string]any{}
	if seg != nil {
		meta["cameraId"] = seg.CameraId
		meta["startedAt"] = seg.StartedAt
		meta["endedAt"] = seg.EndedAt
		meta["file"] = filepath.Base(seg.FilePath)
	}
	target := strconv.FormatUint(id, 10)
	if err := a.serv.DeleteSegment(r.Context(), id); err != nil {
		a.audit.Failure(r, services.ActionRecordingDelete, services.TargetRecording, target, err.Error(), meta)
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	a.audit.Success(r, services.ActionRecordingDelete, services.TargetRecording, target, "deleted one recording segment", meta)
	controllers.SendResult(w, map[string]uint64{"deleted": 1}, "succeed")
}

func (a *recordingApi) downloadSegment(w http.ResponseWriter, r *http.Request) {
	id, ok := readRecordingID(w, r)
	if !ok {
		return
	}
	seg, err := a.serv.GetSegmentById(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	if seg == nil {
		controllers.SendError(w, controllers.ErrBadRequest, "segment not found")
		return
	}

	wantTranscode := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("transcode")), "h264") &&
		strings.EqualFold(seg.Codec, "hevc")
	hasRange := strings.TrimSpace(r.Header.Get("Range")) != ""

	// "Who watched this footage" is the question this route answers, and it is the one a
	// tender and a GDPR Article 30 record both ask for. Recorded once here, before the
	// range/transcode branching below, so every path through the handler is covered.
	//
	// A ranged request is a scrubbing <video> element rather than a distinct viewing, so
	// only the FIRST range of a playback is worth an entry — otherwise seeking through one
	// clip writes dozens of rows and buries the trail it is meant to provide. The opening
	// request of any playback is unranged, which is what makes that split reliable.
	action := services.ActionRecordingDownload
	if hasRange {
		action = services.ActionRecordingView
	}
	if !hasRange || strings.HasPrefix(strings.TrimSpace(r.Header.Get("Range")), "bytes=0-") {
		a.audit.Success(r, action, services.TargetRecording, strconv.FormatInt(seg.Id, 10),
			fmt.Sprintf("camera %d, %s to %s", seg.CameraId,
				time.Unix(seg.StartedAt, 0).UTC().Format(time.RFC3339),
				time.Unix(seg.EndedAt, 0).UTC().Format(time.RFC3339)),
			map[string]any{"cameraId": seg.CameraId, "startedAt": seg.StartedAt, "endedAt": seg.EndedAt})
	}

	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Disposition", `inline; filename="`+filepath.Base(seg.FilePath)+`"`)

	// A Range request (control-plane tunnel playback, or a <video> element seeking) needs a
	// SEEKABLE source so http.ServeContent can answer 206 Partial Content. Plaintext files
	// are already seekable; encrypted or transcoded clips have non-seekable decode streams,
	// so materialize a plaintext (optionally H.264) copy to a short-lived temp cache first.
	// This is what makes recording playback work over the tunnel (its per-message cap can't
	// carry a whole clip — the control plane fetches bounded byte ranges instead).
	if hasRange {
		var seekable io.ReadSeeker
		closeFn := func() {}
		if a.cipher == nil && !wantTranscode {
			pf, oerr := os.Open(seg.FilePath)
			if oerr != nil {
				controllers.SendError(w, controllers.ErrBadRequest, "video file not available")
				return
			}
			seekable, closeFn = pf, func() { pf.Close() }
		} else {
			path, perr := a.segmentPlayFile(r, seg.FilePath, seg.Id, wantTranscode)
			if perr != nil {
				controllers.SendError(w, controllers.ErrInternalServerError, "prepare playback failed")
				return
			}
			pf, oerr := os.Open(path)
			if oerr != nil {
				controllers.SendError(w, controllers.ErrBadRequest, "video file not available")
				return
			}
			seekable, closeFn = pf, func() { pf.Close() }
		}
		defer closeFn()
		modtime := time.Time{}
		if seg.StartedAt > 0 {
			modtime = time.Unix(seg.StartedAt, 0)
		}
		http.ServeContent(w, r, filepath.Base(seg.FilePath), modtime, seekable)
		return
	}

	// Non-range: stream the whole clip, decrypting (and transcoding HEVC on request) on
	// the fly. The stored FileSize is ciphertext size, so Content-Length is only the
	// plaintext pass-through case.
	f, err := os.Open(seg.FilePath)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, "video file not available")
		return
	}
	defer f.Close()
	var src io.Reader = f
	plaintextPassthrough := true
	if a.cipher != nil {
		dr, derr := a.cipher.MaybeDecryptingReader(f)
		if derr != nil {
			controllers.SendError(w, controllers.ErrInternalServerError, "decrypt failed")
			return
		}
		src = dr
		plaintextPassthrough = false
	}
	if wantTranscode {
		if err := recording.TranscodeH264(r.Context(), a.recordFFmpegPath(r), src, w); err != nil {
			log.Printf("recording: transcode segment %d to h264: %v", seg.Id, err)
		}
		return
	}
	if plaintextPassthrough && seg.FileSize > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(seg.FileSize, 10))
	}
	io.Copy(w, src)
}

// segmentPlayFile materializes a seekable, plaintext (optionally H.264-transcoded) copy of
// a segment to a short-lived temp cache, so http.ServeContent can serve Range requests for
// clips whose on-disk form is encrypted and/or HEVC. The cached file is reused on later
// requests; stale files are swept after an hour.
func (a *recordingApi) segmentPlayFile(r *http.Request, filePath string, segID int64, transcode bool) (string, error) {
	cacheDir := filepath.Join(os.TempDir(), "mymatasan-playcache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", err
	}
	a.cleanupPlayCache(cacheDir)
	suffix := ""
	if transcode {
		suffix = "_h264"
	}
	path := filepath.Join(cacheDir, fmt.Sprintf("seg_%d%s.mp4", segID, suffix))
	if fi, err := os.Stat(path); err == nil && fi.Size() > 0 {
		return path, nil
	}
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	var src io.Reader = f
	if a.cipher != nil {
		dr, derr := a.cipher.MaybeDecryptingReader(f)
		if derr != nil {
			return "", derr
		}
		src = dr
	}
	tmp := path + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	if transcode {
		err = recording.TranscodeH264(r.Context(), a.recordFFmpegPath(r), src, out)
	} else {
		_, err = io.Copy(out, src)
	}
	out.Close()
	if err != nil {
		os.Remove(tmp)
		return "", err
	}
	if rerr := os.Rename(tmp, path); rerr != nil {
		os.Remove(tmp)
		return "", rerr
	}
	return path, nil
}

// cleanupPlayCache best-effort removes materialized playback files older than an hour.
func (a *recordingApi) cleanupPlayCache(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-time.Hour)
	for _, e := range entries {
		if info, ierr := e.Info(); ierr == nil && info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// segmentFrame returns a small JPEG thumbnail of the segment at ?seek=<seconds>, with
// the detection box drawn on it (?box=x,y,w,h&label=...). The extracted frame depends
// only on (segment, seek), so it is cached on disk; the box is drawn per request (it
// is cheap and keeps the cache box-agnostic). Backs the Object Search result previews.
func (a *recordingApi) segmentFrame(w http.ResponseWriter, r *http.Request) {
	id, ok := readRecordingID(w, r)
	if !ok {
		return
	}
	seg, err := a.serv.GetSegmentById(r.Context(), id)
	if err != nil || seg == nil {
		controllers.SendError(w, controllers.ErrBadRequest, "segment not found")
		return
	}
	seek := parseInt64Query(r, "seek")
	if seek < 0 {
		seek = 0
	}
	// Width: small thumbnail by default, a larger frame for the maximized view. Clamped
	// so a caller can't request an unbounded render.
	width := int(parseInt64Query(r, "w"))
	if width <= 0 {
		width = 480
	}
	if width < 160 {
		width = 160
	}
	if width > 1920 {
		width = 1920
	}

	cacheDir := filepath.Join(os.TempDir(), "mymatasan-thumbs")
	_ = os.MkdirAll(cacheDir, 0o755)
	cachePath := filepath.Join(cacheDir, fmt.Sprintf("%d_%d_%d.jpg", seg.Id, seek, width))

	frame, rerr := os.ReadFile(cachePath)
	if rerr != nil || len(frame) == 0 {
		f, oerr := os.Open(seg.FilePath)
		if oerr != nil {
			controllers.SendError(w, controllers.ErrBadRequest, "video file not available")
			return
		}
		defer f.Close()
		var src io.Reader = f
		if a.cipher != nil {
			dr, derr := a.cipher.MaybeDecryptingReader(f)
			if derr != nil {
				controllers.SendError(w, controllers.ErrInternalServerError, "decrypt failed")
				return
			}
			src = dr
		}
		frame, err = recording.ExtractFrameJPEG(r.Context(), a.recordFFmpegPath(r), src, seek, width)
		if err != nil || len(frame) == 0 {
			controllers.SendError(w, controllers.ErrInternalServerError, "frame extract failed")
			return
		}
		_ = os.WriteFile(cachePath, frame, 0o644)
	}

	if box, ok := parseBoxQuery(r.URL.Query().Get("box")); ok {
		label := strings.TrimSpace(r.URL.Query().Get("label"))
		frame = vision.AnnotateJPEG(frame, []vision.AnnotatedBox{{Box: box, Label: label}}, 82)
	}

	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Write(frame)
}

// parseBoxQuery parses a "x,y,w,h" query value (normalized 0..1) into a vision.Box.
func parseBoxQuery(raw string) (vision.Box, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return vision.Box{}, false
	}
	parts := strings.Split(raw, ",")
	if len(parts) != 4 {
		return vision.Box{}, false
	}
	vals := make([]float64, 4)
	for i, p := range parts {
		v, perr := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if perr != nil {
			return vision.Box{}, false
		}
		vals[i] = v
	}
	if vals[2] <= 0 || vals[3] <= 0 {
		return vision.Box{}, false
	}
	return vision.Box{X: vals[0], Y: vals[1], W: vals[2], H: vals[3]}, true
}

// recordFFmpegPath resolves the ffmpeg binary the serve-time transcode should use,
// mirroring the recorder's source (runtime decoder settings). Empty falls back to
// PATH inside the recording package.
func (a *recordingApi) recordFFmpegPath(r *http.Request) string {
	if a.settings == nil {
		return ""
	}
	if dec, err := a.settings.Decoder(r.Context()); err == nil {
		return dec.MJPEG.FFmpegPath
	}
	return ""
}

func (a *recordingApi) listConfigs(w http.ResponseWriter, r *http.Request) {
	cfgs, err := a.serv.ListConfigs(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, cfgs, "succeed")
}

func (a *recordingApi) getConfig(w http.ResponseWriter, r *http.Request) {
	cameraId, err := strconv.ParseInt(mux.Vars(r)["cameraId"], 10, 64)
	if err != nil || cameraId <= 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid cameraId")
		return
	}
	cfg, err := a.serv.GetConfig(r.Context(), cameraId)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, cfg, "succeed")
}

func (a *recordingApi) saveConfig(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var body services.SaveRecordingConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	// The BEFORE value of retention, captured before the save. Shortening retention is a
	// slower way of deleting footage, so the change belongs in the same trail as the
	// deletions — and only the before/after pair says whether footage was given up.
	prevRetention := 0
	prevEnabled := false
	if body.CameraId > 0 {
		if before, berr := a.serv.GetConfig(r.Context(), body.CameraId); berr == nil && before != nil {
			prevRetention = before.RetentionDays
			prevEnabled = before.Enabled
		}
	}
	cfg, err := a.serv.SaveConfig(r.Context(), body)
	if err != nil {
		a.audit.Failure(r, services.ActionRecordingConfigChange, services.TargetCamera,
			strconv.FormatInt(body.CameraId, 10), err.Error(), nil)
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	if cfg != nil {
		a.audit.Success(r, services.ActionRecordingConfigChange, services.TargetCamera,
			strconv.FormatInt(cfg.CameraId, 10),
			fmt.Sprintf("recording %s, retention %d -> %d days",
				map[bool]string{true: "enabled", false: "disabled"}[cfg.Enabled], prevRetention, cfg.RetentionDays),
			map[string]any{
				"retentionDaysBefore": prevRetention,
				"retentionDaysAfter":  cfg.RetentionDays,
				"enabledBefore":       prevEnabled,
				"enabledAfter":        cfg.Enabled,
			})
	}

	// Hot-reload the recorder so the new config takes effect immediately without restart.
	// The RecorderConfig is built by the one builder every site shares — hand-rolling it
	// here is how ShredPasses was silently dropped once already.
	recorderWarning := ""
	if a.recorder != nil && cfg != nil && a.recorderCfg != nil {
		recCfg, warning := a.recorderCfg.ForRecording(r.Context(), cfg)
		if warning != "" {
			recorderWarning = warning
			log.Printf("recording: cam%d: %s", cfg.CameraId, warning)
		} else if cerr := a.recorder.Configure(recCfg); cerr != nil {
			recorderWarning = cerr.Error()
			log.Printf("recording: configure cam%d: %v", cfg.CameraId, cerr)
		}
	}

	controllers.SendResult(w, map[string]any{
		"config":          cfg,
		"recorderWarning": recorderWarning,
	}, "succeed")
}

func (a *recordingApi) recorderStatus(w http.ResponseWriter, r *http.Request) {
	if a.recorder == nil {
		controllers.SendResult(w, []any{}, "succeed")
		return
	}
	controllers.SendResult(w, a.recorder.Statuses(), "succeed")
}

// storageStatus reports whether the configured at-rest storage codec can actually be
// produced on this host. A re-encode codec (h264/hevc) needs a working NVENC GPU
// encoder; when none is present the recorder stores segments as plain stream-copy
// (if fallback is on) or drops them (if off). The UI uses `compatible=false` to warn
// the operator that the storage codec setting doesn't match the hardware.
func (a *recordingApi) storageStatus(w http.ResponseWriter, r *http.Request) {
	codec := "copy"
	fallback := true
	if a.settings != nil {
		if rec, err := a.settings.Recording(r.Context()); err == nil {
			if c := strings.ToLower(strings.TrimSpace(rec.Storage.Codec)); c != "" {
				codec = c
			}
			fallback = rec.Storage.FallbackToCopy == nil || *rec.Storage.FallbackToCopy
		}
	}
	reEncode := recording.ReEncodes(codec)
	usable := true
	if reEncode {
		usable = recording.StorageCodecUsable(a.recordFFmpegPath(r), codec)
	}
	controllers.SendResult(w, map[string]any{
		"codec":          codec,
		"reEncode":       reEncode,
		"nvencUsable":    usable,
		"fallbackToCopy": fallback,
		"compatible":     !reEncode || usable,
	}, "succeed")
}

// listCameraStreams returns all ONVIF stream profiles for a camera using stored credentials.
func (a *recordingApi) listCameraStreams(w http.ResponseWriter, r *http.Request) {
	cameraId, err := strconv.ParseUint(mux.Vars(r)["cameraId"], 10, 64)
	if err != nil || cameraId == 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid cameraId")
		return
	}
	if a.camera == nil {
		controllers.SendResult(w, nil, "succeed")
		return
	}
	// A camera that simply is not there is a 404, not a server fault. StreamOptions
	// loads the device record first and returns a plain error when that lookup misses,
	// which this handler used to map to 500 — so probing any unknown cameraId answered
	// "internal server error". Separate the two so a 500 here once again means the ONVIF
	// call itself failed.
	if detail, err := a.camera.GetById(r.Context(), cameraId); err != nil || detail == nil {
		controllers.SendError(w, controllers.ErrNotFound, "camera not found")
		return
	}
	// Empty credentials → service falls back to credentials stored in the device record.
	result, err := a.camera.StreamOptions(r.Context(), cameraId, onvif.Credentials{})
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, result, "succeed")
}

// setLiveStream updates the camera's configured live-view stream URI.
func (a *recordingApi) setLiveStream(w http.ResponseWriter, r *http.Request) {
	cameraId, err := strconv.ParseUint(mux.Vars(r)["cameraId"], 10, 64)
	if err != nil || cameraId == 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid cameraId")
		return
	}
	var body struct {
		RTSPURL string `json:"rtspUrl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	if strings.TrimSpace(body.RTSPURL) == "" {
		controllers.SendError(w, controllers.ErrBadRequest, "rtspUrl is required")
		return
	}
	if a.camera == nil {
		controllers.SendError(w, controllers.ErrInternalServerError, "onvif service unavailable")
		return
	}
	// Store the URL directly — no ONVIF roundtrip or RTSP probe needed since the
	// caller already chose it from the detect-streams list.
	device, err := a.camera.SetLiveStream(r.Context(), cameraId, body.RTSPURL)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, device, "succeed")
}

func readRecordingID(w http.ResponseWriter, r *http.Request) (uint64, bool) {
	id, err := strconv.ParseUint(mux.Vars(r)["id"], 10, 64)
	if err != nil || id == 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}

func parseInt64Query(r *http.Request, key string) int64 {
	v, _ := strconv.ParseInt(r.URL.Query().Get(key), 10, 64)
	return v
}
