package apis

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/infra/onvif"
	"github.com/mysayasan/kopiv2/infra/rtsp"
	"github.com/mysayasan/kopiv2/infra/stream"
)

type cameraApi struct {
	serv          services.ICameraService
	settings      services.IRuntimeSettingsService
	streamManager *stream.Manager
	healthProber  services.ICameraHealthProber
}

type saveDiscoveredRequest struct {
	onvif.Device
	Description string `json:"description"`
	// Credentials to verify before the camera is saved (an ONVIF/RTSP login that the
	// camera actively rejects blocks the save).
	Username string `json:"username"`
	Password string `json:"password"`
}

type streamURIRequest struct {
	Username     string `json:"username"`
	Password     string `json:"password"`
	ProfileToken string `json:"profileToken"`
	RTSPURL      string `json:"rtspUrl"`
}

type cameraPasswordRequest struct {
	CurrentUsername string `json:"currentUsername"`
	CurrentPassword string `json:"currentPassword"`
	TargetUsername  string `json:"targetUsername"`
	NewPassword     string `json:"newPassword"`
	UserLevel       string `json:"userLevel"`
}

type updateDetailsRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type ptzMoveRequest struct {
	Direction  string  `json:"direction"`
	Speed      float64 `json:"speed"`
	DurationMs int64   `json:"durationMs"`
}

type webRTCOfferRequest struct {
	Type string `json:"type"`
	SDP  string `json:"sdp"`
	// RTSPURL, when set, previews a specific detected stream instead of the camera's
	// active one — streamed under a separate source ID with no change to the camera.
	RTSPURL string `json:"rtspUrl,omitempty"`
}

// streamTestRequest optionally targets a specific detected stream URL for a per-stream
// connectivity test; empty rtspUrl tests the camera's active stream.
type streamTestRequest struct {
	RTSPURL string `json:"rtspUrl,omitempty"`
}

type cameraEncoderRequest struct {
	Encoding         string `json:"encoding"`
	BitrateLimitKbps int    `json:"bitrateLimitKbps"`
}

// NewCameraApi registers camera CRUD, streaming, and PTZ routes under /cameras.
func NewCameraApi(router *mux.Router, serv services.ICameraService, settings services.IRuntimeSettingsService, streamManager *stream.Manager, healthProber services.ICameraHealthProber) {
	handler := &cameraApi{serv: serv, settings: settings, streamManager: streamManager, healthProber: healthProber}
	group := router.PathPrefix("/cameras").Subrouter()

	group.HandleFunc("", handler.get).Methods("GET")
	group.HandleFunc("/health/refresh", handler.refreshHealth).Methods("POST")
	group.HandleFunc("/discovered", handler.saveDiscovered).Methods("POST")
	group.HandleFunc("/{id}/credentials", handler.saveCredentials).Methods("POST")
	group.HandleFunc("/{id}/auth-check", handler.authCheck).Methods("GET")
	group.HandleFunc("/{id}/camera-password", handler.changeCameraPassword).Methods("POST")
	group.HandleFunc("/{id}/onvif-users", handler.listCameraUsers).Methods("GET")
	group.HandleFunc("/{id}/onvif-users", handler.createCameraUser).Methods("POST")
	group.HandleFunc("/{id}/onvif-users/{username}", handler.deleteCameraUser).Methods("DELETE")
	group.HandleFunc("/{id}/reboot", handler.rebootCamera).Methods("POST")
	group.HandleFunc("/{id}/factory-default", handler.factoryDefaultCamera).Methods("POST")
	group.HandleFunc("/{id}/datetime", handler.getCameraDateTime).Methods("GET")
	group.HandleFunc("/{id}/datetime", handler.setCameraDateTime).Methods("POST")
	group.HandleFunc("/{id}/network", handler.getCameraNetwork).Methods("GET")
	group.HandleFunc("/{id}/network", handler.setCameraNetwork).Methods("POST")
	group.HandleFunc("/{id}/capabilities", handler.getCameraCapabilities).Methods("GET")
	group.HandleFunc("/{id}/device-info", handler.getCameraDeviceInfo).Methods("GET")
	group.HandleFunc("/{id}/stream-options", handler.streamOptions).Methods("POST")
	group.HandleFunc("/{id}/stream-uri", handler.resolveStream).Methods("POST")
	group.HandleFunc("/{id}/rtsp-test", handler.testStream).Methods("POST")
	group.HandleFunc("/{id}/live-view", handler.resolveLiveView).Methods("POST")
	group.HandleFunc("/{id}/ptz/move", handler.ptzMove).Methods("POST")
	group.HandleFunc("/{id}/ptz/stop", handler.ptzStop).Methods("POST")
	group.HandleFunc("/{id}/encoder", handler.getEncoder).Methods("GET")
	group.HandleFunc("/{id}/encoder", handler.applyEncoder).Methods("POST")
	group.HandleFunc("/{id}/lpr-capability", handler.lprCapability).Methods("GET")
	group.HandleFunc("/{id}/webrtc/offer", handler.createWebRTCAnswer).Methods("POST")
	group.HandleFunc("/{id}/live.mjpeg", handler.liveMJPEG).Methods("GET")
	group.HandleFunc("/{id}", handler.updateDetails).Methods("PUT")
	group.HandleFunc("/{id}", handler.delete).Methods("DELETE")
}

func (a *cameraApi) get(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.ParseUint(r.URL.Query().Get("limit"), 10, 64)
	offset, _ := strconv.ParseUint(r.URL.Query().Get("offset"), 10, 64)
	res, totalCnt, err := a.serv.Get(r.Context(), limit, offset)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendPagingResult(w, res, limit, offset, totalCnt)
}

// refreshHealth runs an immediate concurrent reachability probe of all cameras
// and returns fresh per-camera status, so the UI can flag offline cameras at
// login without waiting for the debounced background sweep.
func (a *cameraApi) refreshHealth(w http.ResponseWriter, r *http.Request) {
	if a.healthProber == nil {
		controllers.SendResult(w, []services.CameraHealthSnapshot{}, "succeed")
		return
	}
	results := a.healthProber.ProbeAllNow(r.Context())
	controllers.SendResult(w, results, "succeed")
}

func (a *cameraApi) saveDiscovered(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1048576)
	var body saveDiscoveredRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	detail := services.CameraDetail{
		Camera:       services.CameraFromDevice(body.Device, body.Name, body.Description),
		XAddr:        body.XAddr,
		Types:        strings.Join(body.Types, " "),
		Scopes:       strings.Join(body.Scopes, " "),
		HardwareID:   body.HardwareID,
		MediaXAddr:   body.MediaXAddr,
		PTZXAddr:     body.PTZXAddr,
		PTZSupported: body.PTZSupported,
		ProfileToken: body.ProfileToken,
	}
	// Require working credentials before saving: a camera that actively rejects the login
	// can't be added (an unreachable camera is allowed through — we just couldn't verify).
	creds := onvif.Credentials{Username: strings.TrimSpace(body.Username), Password: body.Password}
	if status, _ := a.serv.VerifyDeviceCredentials(r.Context(), detail, creds); status == services.CameraAuthUnauthorized {
		controllers.SendError(w, controllers.ErrBadRequest, "camera rejected the credentials")
		return
	}
	detail.Username = creds.Username
	detail.Password = creds.Password
	res, err := a.serv.Save(r.Context(), detail)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, res, "succeed")
}

func (a *cameraApi) updateDetails(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	id, err := strconv.ParseUint(params["id"], 10, 64)
	if err != nil || id == 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid id")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 65536)
	var body updateDetailsRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	detail, err := a.serv.GetById(r.Context(), id)
	if err != nil || detail == nil {
		controllers.SendError(w, controllers.ErrNotFound, "camera not found")
		return
	}
	detail.Camera.Name = strings.TrimSpace(body.Name)
	detail.Camera.Description = strings.TrimSpace(body.Description)
	if _, err := a.serv.Save(r.Context(), *detail); err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, nil, "succeed")
}

func (a *cameraApi) saveCredentials(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1048576)
	params := mux.Vars(r)
	id, _ := strconv.ParseUint(params["id"], 10, 64)

	var body streamURIRequest
	if r.Body != nil {
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			controllers.SendError(w, controllers.ErrParseFailed, err.Error())
			return
		}
	}
	res, err := a.serv.SaveCredentials(r.Context(), id, onvif.Credentials{
		Username: body.Username,
		Password: body.Password,
	})
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, res, "succeed")
}

// authCheck reports whether a camera's stored credentials still authenticate — the camera
// node's access gate uses "unauthorized" to prompt for new credentials.
func (a *cameraApi) authCheck(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	id, _ := strconv.ParseUint(params["id"], 10, 64)
	status, err := a.serv.CameraAuthStatus(r.Context(), id)
	if err != nil {
		sendCameraBadRequest(w, err)
		return
	}
	controllers.SendResult(w, map[string]string{"status": status}, "succeed")
}

func (a *cameraApi) changeCameraPassword(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1048576)
	params := mux.Vars(r)
	id, _ := strconv.ParseUint(params["id"], 10, 64)

	var body cameraPasswordRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	res, err := a.serv.ChangeCameraPassword(r.Context(), id, services.ChangeCameraPasswordRequest{
		CurrentUsername: body.CurrentUsername,
		CurrentPassword: body.CurrentPassword,
		TargetUsername:  body.TargetUsername,
		NewPassword:     body.NewPassword,
		UserLevel:       body.UserLevel,
	})
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, res, "succeed")
}

type cameraUserRequest struct {
	Username  string `json:"username"`
	Password  string `json:"password"`
	UserLevel string `json:"userLevel"`
}

func (a *cameraApi) listCameraUsers(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	id, _ := strconv.ParseUint(params["id"], 10, 64)
	res, err := a.serv.ListCameraUsers(r.Context(), id)
	if err != nil {
		sendCameraBadRequest(w, err)
		return
	}
	controllers.SendResult(w, res, "succeed")
}

func (a *cameraApi) createCameraUser(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1048576)
	params := mux.Vars(r)
	id, _ := strconv.ParseUint(params["id"], 10, 64)
	var body cameraUserRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	if err := a.serv.CreateCameraUser(r.Context(), id, services.CreateCameraUserRequest{
		Username:  body.Username,
		Password:  body.Password,
		UserLevel: body.UserLevel,
	}); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	// Return the refreshed list so the UI reflects the new user immediately.
	users, err := a.serv.ListCameraUsers(r.Context(), id)
	if err != nil {
		controllers.SendResult(w, []onvif.User{}, "succeed")
		return
	}
	controllers.SendResult(w, users, "succeed")
}

func (a *cameraApi) deleteCameraUser(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	id, _ := strconv.ParseUint(params["id"], 10, 64)
	if err := a.serv.DeleteCameraUser(r.Context(), id, params["username"]); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	users, err := a.serv.ListCameraUsers(r.Context(), id)
	if err != nil {
		controllers.SendResult(w, []onvif.User{}, "succeed")
		return
	}
	controllers.SendResult(w, users, "succeed")
}

type factoryDefaultRequest struct {
	Hard bool `json:"hard"`
}

func (a *cameraApi) rebootCamera(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	id, _ := strconv.ParseUint(params["id"], 10, 64)
	msg, err := a.serv.RebootCamera(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]string{"message": msg}, "succeed")
}

func (a *cameraApi) factoryDefaultCamera(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	id, _ := strconv.ParseUint(params["id"], 10, 64)
	var body factoryDefaultRequest
	if r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, 1048576)
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil && err != io.EOF {
			controllers.SendError(w, controllers.ErrParseFailed, err.Error())
			return
		}
	}
	if err := a.serv.FactoryDefaultCamera(r.Context(), id, body.Hard); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]bool{"ok": true}, "succeed")
}

func (a *cameraApi) getCameraDateTime(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	id, _ := strconv.ParseUint(params["id"], 10, 64)
	res, err := a.serv.GetCameraDateTime(r.Context(), id)
	if err != nil {
		sendCameraBadRequest(w, err)
		return
	}
	controllers.SendResult(w, res, "succeed")
}

func (a *cameraApi) setCameraDateTime(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1048576)
	params := mux.Vars(r)
	id, _ := strconv.ParseUint(params["id"], 10, 64)
	var body services.SetCameraDateTimeRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	if err := a.serv.SetCameraDateTime(r.Context(), id, body); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	res, err := a.serv.GetCameraDateTime(r.Context(), id)
	if err != nil {
		controllers.SendResult(w, map[string]bool{"ok": true}, "succeed")
		return
	}
	controllers.SendResult(w, res, "succeed")
}

func (a *cameraApi) getCameraNetwork(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	id, _ := strconv.ParseUint(params["id"], 10, 64)
	res, err := a.serv.GetCameraNetwork(r.Context(), id)
	if err != nil {
		sendCameraBadRequest(w, err)
		return
	}
	controllers.SendResult(w, res, "succeed")
}

func (a *cameraApi) getCameraCapabilities(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	id, _ := strconv.ParseUint(params["id"], 10, 64)
	res, err := a.serv.GetCameraCapabilities(r.Context(), id)
	if err != nil {
		sendCameraBadRequest(w, err)
		return
	}
	controllers.SendResult(w, res, "succeed")
}

func (a *cameraApi) getCameraDeviceInfo(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	id, _ := strconv.ParseUint(params["id"], 10, 64)
	res, err := a.serv.GetCameraDeviceInfo(r.Context(), id)
	if err != nil {
		sendCameraBadRequest(w, err)
		return
	}
	controllers.SendResult(w, res, "succeed")
}

func (a *cameraApi) setCameraNetwork(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1048576)
	params := mux.Vars(r)
	id, _ := strconv.ParseUint(params["id"], 10, 64)
	var body services.SetCameraNetworkRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	if err := a.serv.SetCameraNetwork(r.Context(), id, body); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]bool{"ok": true}, "succeed")
}

func (a *cameraApi) streamOptions(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1048576)
	params := mux.Vars(r)
	id, _ := strconv.ParseUint(params["id"], 10, 64)

	var body streamURIRequest
	if r.Body != nil {
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			controllers.SendError(w, controllers.ErrParseFailed, err.Error())
			return
		}
	}
	res, err := a.serv.StreamOptions(r.Context(), id, onvif.Credentials{
		Username: body.Username,
		Password: body.Password,
	})
	if err != nil {
		sendCameraBadRequest(w, err)
		return
	}
	controllers.SendResult(w, res, "succeed")
}

func (a *cameraApi) resolveStream(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1048576)
	params := mux.Vars(r)
	id, _ := strconv.ParseUint(params["id"], 10, 64)

	var body streamURIRequest
	if r.Body != nil {
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			controllers.SendError(w, controllers.ErrParseFailed, err.Error())
			return
		}
	}
	res, err := a.serv.ResolveStream(r.Context(), id, services.StreamSelectionRequest{
		Credentials: onvif.Credentials{
			Username: body.Username,
			Password: body.Password,
		},
		ProfileToken: body.ProfileToken,
		RTSPURL:      body.RTSPURL,
	})
	if err != nil {
		sendCameraBadRequest(w, err)
		return
	}
	controllers.SendResult(w, res, "succeed")
}

func (a *cameraApi) testStream(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	id, _ := strconv.ParseUint(params["id"], 10, 64)
	// A specific detected stream (rtspUrl) is probed without persisting; empty tests the
	// camera's active stream.
	var body streamTestRequest
	if r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, 1048576)
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil && err != io.EOF {
			controllers.SendError(w, controllers.ErrParseFailed, err.Error())
			return
		}
	}
	var res *rtsp.ProbeResult
	var err error
	if strings.TrimSpace(body.RTSPURL) != "" {
		res, err = a.serv.TestStreamURL(r.Context(), id, body.RTSPURL)
	} else {
		res, err = a.serv.TestStream(r.Context(), id)
	}
	if err != nil {
		sendCameraBadRequest(w, err)
		return
	}
	controllers.SendResult(w, res, "succeed")
}

func (a *cameraApi) resolveLiveView(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1048576)
	params := mux.Vars(r)
	id, _ := strconv.ParseUint(params["id"], 10, 64)

	var body streamURIRequest
	if r.Body != nil {
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			controllers.SendError(w, controllers.ErrParseFailed, err.Error())
			return
		}
	}
	res, err := a.serv.ResolveLiveView(r.Context(), id, onvif.Credentials{
		Username: body.Username,
		Password: body.Password,
	})
	if err != nil {
		sendCameraBadRequest(w, err)
		return
	}
	controllers.SendResult(w, res, "succeed")
}

func (a *cameraApi) ptzMove(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1048576)
	params := mux.Vars(r)
	id, _ := strconv.ParseUint(params["id"], 10, 64)

	var body ptzMoveRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	res, err := a.serv.PTZMove(r.Context(), id, services.PTZMoveRequest{
		Direction:  body.Direction,
		Speed:      body.Speed,
		DurationMs: body.DurationMs,
	})
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, res, "succeed")
}

func (a *cameraApi) ptzStop(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	id, _ := strconv.ParseUint(params["id"], 10, 64)
	res, err := a.serv.PTZStop(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, res, "succeed")
}

// getEncoder reads the camera's current ONVIF video encoder configuration so the UI
// can show the live codec/bitrate/resolution for the recording profile.
// lprCapability reports whether a camera is suitable for license-plate recognition
// (used to gate the LPR rule option in the UI). Best-effort + cached server-side.
func (a *cameraApi) lprCapability(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	id, _ := strconv.ParseUint(params["id"], 10, 64)
	controllers.SendResult(w, a.serv.LPRCapability(r.Context(), int64(id)), "succeed")
}

func (a *cameraApi) getEncoder(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	id, _ := strconv.ParseUint(params["id"], 10, 64)
	cfg, err := a.serv.GetCameraEncoder(r.Context(), id)
	if err != nil {
		// Pass the descriptive error itself so the camera's actual reason reaches the
		// client (SendError only surfaces the generic status text otherwise).
		controllers.SendError(w, err, err.Error())
		return
	}
	controllers.SendResult(w, cfg, "succeed")
}

// applyEncoder pushes a recording codec (+ optional bitrate cap) to the camera's
// encoder via ONVIF — the zero-host-cost compression lever (camera encodes smaller
// H.265; the host keeps recording with -c copy).
func (a *cameraApi) applyEncoder(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1048576)
	params := mux.Vars(r)
	id, _ := strconv.ParseUint(params["id"], 10, 64)

	var body cameraEncoderRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	cfg, err := a.serv.ApplyCameraEncoder(r.Context(), id, services.ApplyCameraEncoderRequest{
		Encoding:         body.Encoding,
		BitrateLimitKbps: body.BitrateLimitKbps,
	})
	if err != nil {
		// Surface the camera's actual reason (e.g. "did not apply H265 — it kept H264")
		// rather than the generic "bad request" text.
		controllers.SendError(w, err, err.Error())
		return
	}
	controllers.SendResult(w, cfg, "succeed")
}

func (a *cameraApi) createWebRTCAnswer(w http.ResponseWriter, r *http.Request) {
	streamSettings, err := a.settings.Stream(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	if !streamSettings.WebRTC.Enabled {
		controllers.SendError(w, controllers.ErrBadRequest, "webrtc live view is disabled")
		return
	}
	if a.streamManager == nil {
		controllers.SendError(w, controllers.ErrBadRequest, "stream manager is not configured")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 2*1024*1024)
	params := mux.Vars(r)
	id, _ := strconv.ParseUint(params["id"], 10, 64)

	var body webRTCOfferRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}

	// A specific detected stream is previewed under a SEPARATE source ID with the
	// camera's credentials but no DB change, so the camera's active stream (used by
	// recording + detection siphon) is never disturbed. Empty rtspUrl = the active stream.
	var source services.SnapshotSource
	sourceID := fmt.Sprintf("camera-%d", id)
	if strings.TrimSpace(body.RTSPURL) != "" {
		source, err = a.serv.PreviewSource(r.Context(), id, body.RTSPURL)
		sourceID = fmt.Sprintf("camera-%d-preview", id)
	} else {
		source, err = a.serv.SnapshotSource(r.Context(), id)
	}
	if err != nil {
		sendCameraBadRequest(w, err)
		return
	}
	if strings.TrimSpace(source.RTSPURI) == "" {
		controllers.SendError(w, controllers.ErrBadRequest, "rtspUrl is required; resolve live view first")
		return
	}

	answer, err := a.streamManager.CreateWebRTCAnswerWithOptions(r.Context(), stream.Source{
		ID:  sourceID,
		URI: source.RTSPURI,
	}, stream.SessionDescription{
		Type: body.Type,
		SDP:  body.SDP,
	}, stream.Options{
		ICEServers: streamSettings.WebRTC.ICEServers,
	})
	if err != nil {
		sendCameraBadRequest(w, err)
		return
	}
	controllers.SendResult(w, answer, "succeed")
}

func (a *cameraApi) liveMJPEG(w http.ResponseWriter, r *http.Request) {
	runtimeSettings, err := a.settings.Get(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	if !runtimeSettings.Stream.MJPEGFallback.Enabled {
		controllers.SendError(w, controllers.ErrBadRequest, "mjpeg fallback live view is disabled")
		return
	}

	params := mux.Vars(r)
	id, _ := strconv.ParseUint(params["id"], 10, 64)

	source, err := a.serv.SnapshotSource(r.Context(), id)
	if err != nil {
		sendCameraBadRequest(w, err)
		return
	}

	fps, _ := strconv.Atoi(r.URL.Query().Get("fps"))
	if fps <= 0 {
		fps = 5
	}
	if fps > 15 {
		fps = 15
	}
	maxWidth, _ := strconv.Atoi(r.URL.Query().Get("width"))
	if maxWidth <= 0 {
		maxWidth = 480
	}
	if maxWidth > 1920 {
		maxWidth = 1920
	}

	sourceMode := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("source")))
	useSnapshot := sourceMode == "snapshot"
	preferSnapshot := useSnapshot || strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("preferSnapshot")), "true") || strings.TrimSpace(r.URL.Query().Get("preferSnapshot")) == "1"
	client := &http.Client{Timeout: 2 * time.Second}
	if preferSnapshot && strings.TrimSpace(source.URI) != "" {
		frame, err := fetchSnapshotFrame(r.Context(), client, source)
		if err == nil && len(frame) > 0 {
			setMJPEGHeaders(w)
			if !writeMJPEGFrame(w, frame) {
				return
			}
			streamSnapshotMJPEG(r.Context(), w, client, source, fps)
			return
		}
		if useSnapshot {
			controllers.SendError(w, controllers.ErrBadRequest, "snapshotUri is not available or did not return a JPEG frame")
			return
		}
	}

	if !useSnapshot && strings.TrimSpace(source.RTSPURI) != "" {
		mjpegOptions := services.MJPEGOptionsFromDecoderSettings(runtimeSettings.Decoder)
		mjpegOptions.FPS = fps
		mjpegOptions.MaxWidth = maxWidth
		setMJPEGHeaders(w)
		if err := rtsp.StreamMJPEG(r.Context(), w, source.RTSPURI, mjpegOptions); err != nil {
			return
		}
		return
	}

	if strings.TrimSpace(source.URI) != "" {
		frame, err := fetchSnapshotFrame(r.Context(), client, source)
		if err == nil && len(frame) > 0 {
			setMJPEGHeaders(w)
			if !writeMJPEGFrame(w, frame) {
				return
			}
			streamSnapshotMJPEG(r.Context(), w, client, source, fps)
			return
		}
	}

	controllers.SendError(w, controllers.ErrBadRequest, "snapshotUri or rtspUrl is required; resolve live view first")
}

func (a *cameraApi) delete(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	id, _ := strconv.ParseUint(params["id"], 10, 64)
	res, err := a.serv.Delete(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, res, "succeed")
}

func sendCameraBadRequest(w http.ResponseWriter, err error) {
	if err == nil {
		controllers.SendError(w, controllers.ErrBadRequest, "bad request")
		return
	}
	controllers.SendError(w, controllers.ErrBadRequest, err.Error(), map[string]string{"error": err.Error()})
}

func streamSnapshotMJPEG(ctx context.Context, w http.ResponseWriter, client *http.Client, source services.SnapshotSource, fps int) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second / time.Duration(fps)):
		}
		frame, err := fetchSnapshotFrame(ctx, client, source)
		if err == nil && len(frame) > 0 && !writeMJPEGFrame(w, frame) {
			return
		}
	}
}

func writeMJPEGFrame(w http.ResponseWriter, frame []byte) bool {
	if _, err := fmt.Fprintf(w, "--mymatasan\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n", len(frame)); err != nil {
		return false
	}
	if _, err := w.Write(frame); err != nil {
		return false
	}
	if _, err := w.Write([]byte("\r\n")); err != nil {
		return false
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	return true
}

func setMJPEGHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=mymatasan")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Connection", "close")
}

func fetchSnapshotFrame(ctx context.Context, client *http.Client, source services.SnapshotSource) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source.URI, nil)
	if err != nil {
		return nil, err
	}
	if source.Username != "" || source.Password != "" {
		req.SetBasicAuth(source.Username, source.Password)
	}
	req.Header.Set("Accept", "image/jpeg,*/*")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized && source.Username != "" {
		challenge := resp.Header.Get("WWW-Authenticate")
		if strings.Contains(strings.ToLower(challenge), "digest") {
			_ = resp.Body.Close()
			return fetchDigestSnapshotFrame(ctx, client, source, challenge)
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("snapshot endpoint returned status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
}

func fetchDigestSnapshotFrame(ctx context.Context, client *http.Client, source services.SnapshotSource, challenge string) ([]byte, error) {
	values := parseDigestChallenge(challenge)
	realm := values["realm"]
	nonce := values["nonce"]
	if realm == "" || nonce == "" {
		return nil, fmt.Errorf("snapshot endpoint returned unsupported digest challenge")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source.URI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "image/jpeg,*/*")
	req.Header.Set("Authorization", digestAuthorization(req, source, values))

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("snapshot endpoint returned status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
}

func digestAuthorization(req *http.Request, source services.SnapshotSource, values map[string]string) string {
	realm := values["realm"]
	nonce := values["nonce"]
	qop := firstDigestQOP(values["qop"])
	uri := req.URL.RequestURI()
	cnonce := md5Hex(fmt.Sprintf("%d:%s", time.Now().UnixNano(), source.Username))
	nc := "00000001"
	ha1 := md5Hex(source.Username + ":" + realm + ":" + source.Password)
	ha2 := md5Hex(req.Method + ":" + uri)
	response := ""
	if qop != "" {
		response = md5Hex(ha1 + ":" + nonce + ":" + nc + ":" + cnonce + ":" + qop + ":" + ha2)
	} else {
		response = md5Hex(ha1 + ":" + nonce + ":" + ha2)
	}
	parts := []string{
		`Digest username="` + source.Username + `"`,
		`realm="` + realm + `"`,
		`nonce="` + nonce + `"`,
		`uri="` + uri + `"`,
		`response="` + response + `"`,
	}
	if values["opaque"] != "" {
		parts = append(parts, `opaque="`+values["opaque"]+`"`)
	}
	if qop != "" {
		parts = append(parts, `qop=`+qop, `nc=`+nc, `cnonce="`+cnonce+`"`)
	}
	if values["algorithm"] != "" {
		parts = append(parts, `algorithm=`+values["algorithm"])
	}
	return strings.Join(parts, ", ")
}

func parseDigestChallenge(challenge string) map[string]string {
	challenge = strings.TrimSpace(challenge)
	challenge = strings.TrimPrefix(challenge, "Digest")
	result := map[string]string{}
	for _, rawPart := range strings.Split(challenge, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(rawPart), "=")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"`)
		result[strings.ToLower(strings.TrimSpace(key))] = value
	}
	return result
}

func firstDigestQOP(value string) string {
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "auth" {
			return part
		}
	}
	return ""
}

func md5Hex(value string) string {
	sum := md5.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}
