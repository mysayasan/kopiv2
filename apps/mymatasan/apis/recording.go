package apis

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/infra/atrest"
	"github.com/mysayasan/kopiv2/infra/onvif"
	"github.com/mysayasan/kopiv2/infra/recording"
)

type recordingApi struct {
	serv     services.IRecordingService
	recorder *recording.Manager
	camera   services.ICameraService
	settings services.IRuntimeSettingsService
	cipher   *atrest.Cipher
}

// NewRecordingApi registers recording routes under /recording.
func NewRecordingApi(router *mux.Router, serv services.IRecordingService, recorder *recording.Manager, camera services.ICameraService, settings services.IRuntimeSettingsService, cipher *atrest.Cipher) {
	h := &recordingApi{serv: serv, recorder: recorder, camera: camera, settings: settings, cipher: cipher}
	g := router.PathPrefix("/recording").Subrouter()

	g.HandleFunc("/segments", h.listSegments).Methods("GET")
	g.HandleFunc("/segments/purge", h.purgeExpired).Methods("POST")
	g.HandleFunc("/segments/{id}", h.deleteSegment).Methods("DELETE")
	g.HandleFunc("/segments/{id}/download", h.downloadSegment).Methods("GET")
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
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]int{"deleted": deleted}, "succeed")
}

func (a *recordingApi) deleteSegment(w http.ResponseWriter, r *http.Request) {
	id, ok := readRecordingID(w, r)
	if !ok {
		return
	}
	if err := a.serv.DeleteSegment(r.Context(), id); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
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

	f, err := os.Open(seg.FilePath)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, "video file not available")
		return
	}
	defer f.Close()

	// Source reader: decrypt on the fly when encryption-at-rest is on, else the raw
	// file. The stored FileSize is the ciphertext size, so Content-Length is only set
	// for the plaintext pass-through path (no decrypt, no transcode).
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

	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Disposition", `inline; filename="`+filepath.Base(seg.FilePath)+`"`)

	// HEVC-on-disk playback for browsers that can't decode it: the player appends
	// ?transcode=h264 when its <video> element can't play hev1/hvc1 (Firefox, older
	// browsers). Transcode the (decrypted) stream to H.264 fragmented MP4 on the fly
	// through the shared NVENC semaphore. Capable browsers omit the flag and stream
	// the stored bytes untouched, so they pay nothing.
	if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("transcode")), "h264") &&
		strings.EqualFold(seg.Codec, "hevc") {
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
	cfg, err := a.serv.SaveConfig(r.Context(), body)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}

	// Hot-reload the recorder so the new config takes effect immediately without restart.
	recorderWarning := ""
	if a.recorder != nil && cfg != nil {
		ffmpegPath := ""
		rtspTransport := ""
		if a.settings != nil {
			if dec, err := a.settings.Decoder(r.Context()); err == nil {
				ffmpegPath = dec.MJPEG.FFmpegPath
				rtspTransport = dec.FFmpeg.RTSPTransport
			}
		}
		// Prefer the explicit StreamURL override; fall back to the ONVIF-discovered URI.
		// Always look up device credentials so they can be injected into bare URLs.
		rtspURI := strings.TrimSpace(cfg.StreamURL)
		fallbackURI := strings.TrimSpace(cfg.FallbackStreamUrl)
		if a.camera != nil {
			if src, err := a.camera.SnapshotSource(r.Context(), uint64(cfg.CameraId)); err == nil {
				if rtspURI == "" {
					rtspURI = src.RTSPURI
				} else {
					rtspURI = services.RTSPURIWithCredentials(rtspURI, src.Username, src.Password)
				}
				fallbackURI = services.RTSPURIWithCredentials(fallbackURI, src.Username, src.Password)
			}
		}
		if rtspURI == "" && cfg.Enabled {
			recorderWarning = "camera has no RTSP URI — recording will not start until an RTSP URI is configured on the camera or a Stream URL override is set"
			log.Printf("recording: cam%d enabled but has no RTSP URI", cfg.CameraId)
		} else {
			siphonFPS, siphonWidth := services.SiphonTeeParams(a.settings)
			siphonHWAccel, siphonHWDevice, siphonInitHWDevice, siphonVideoDecoder := services.SiphonDecoderParams(a.settings)
			// Read the at-rest codec live so a Settings → Recording change takes effect
			// the next time any camera's recording config is saved.
			recStorage, _ := a.settings.Recording(r.Context())
			if cerr := a.recorder.Configure(recording.RecorderConfig{
				CameraId:           cfg.CameraId,
				Enabled:            cfg.Enabled,
				PreRollSec:         cfg.PreRollSec,
				PostRollSec:        cfg.PostRollSec,
				StoragePath:        cfg.StoragePath,
				FFmpegPath:         ffmpegPath,
				RTSPTransport:      rtspTransport,
				RTSPURI:            rtspURI,
				FallbackRTSPURI:    fallbackURI,
				SegmentMinutes:     cfg.SegmentMinutes,
				RetentionDays:      cfg.RetentionDays,
				SiphonFPS:          siphonFPS,
				SiphonWidth:        siphonWidth,
				HWAccel:            siphonHWAccel,
				HWAccelDevice:      siphonHWDevice,
				InitHWDevice:       siphonInitHWDevice,
				VideoDecoder:       siphonVideoDecoder,
				RecordCodec:        recStorage.Storage.Codec,
				RecordQuality:      recStorage.Storage.Quality,
				RecordFallbackCopy: recStorage.Storage.FallbackToCopy == nil || *recStorage.Storage.FallbackToCopy,
				Cipher:             a.cipher,
			}); cerr != nil {
				recorderWarning = cerr.Error()
				log.Printf("recording: configure cam%d: %v", cfg.CameraId, cerr)
			}
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
