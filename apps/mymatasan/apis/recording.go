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
	"github.com/mysayasan/kopiv2/infra/onvif"
	"github.com/mysayasan/kopiv2/infra/recording"
)

type recordingApi struct {
	serv     services.IRecordingService
	recorder *recording.Manager
	camera   services.ICameraService
	settings services.IRuntimeSettingsService
}

// NewRecordingApi registers recording routes under /recording.
func NewRecordingApi(router *mux.Router, serv services.IRecordingService, recorder *recording.Manager, camera services.ICameraService, settings services.IRuntimeSettingsService) {
	h := &recordingApi{serv: serv, recorder: recorder, camera: camera, settings: settings}
	g := router.PathPrefix("/recording").Subrouter()

	g.HandleFunc("/segments", h.listSegments).Methods("GET")
	g.HandleFunc("/segments/{id}", h.deleteSegment).Methods("DELETE")
	g.HandleFunc("/segments/{id}/download", h.downloadSegment).Methods("GET")
	g.HandleFunc("/config", h.listConfigs).Methods("GET")
	g.HandleFunc("/config", h.saveConfig).Methods("PUT")
	g.HandleFunc("/config/{cameraId}", h.getConfig).Methods("GET")
	g.HandleFunc("/status", h.recorderStatus).Methods("GET")
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

	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Disposition", `inline; filename="`+filepath.Base(seg.FilePath)+`"`)
	if seg.FileSize > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(seg.FileSize, 10))
	}
	io.Copy(w, f)
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
		} else if cerr := a.recorder.Configure(recording.RecorderConfig{
			CameraId:        cfg.CameraId,
			Enabled:         cfg.Enabled,
			PreRollSec:      cfg.PreRollSec,
			PostRollSec:     cfg.PostRollSec,
			StoragePath:     cfg.StoragePath,
			FFmpegPath:      ffmpegPath,
			RTSPTransport:   rtspTransport,
			RTSPURI:         rtspURI,
			FallbackRTSPURI: fallbackURI,
			SegmentMinutes:  cfg.SegmentMinutes,
			RetentionDays:   cfg.RetentionDays,
		}); cerr != nil {
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
