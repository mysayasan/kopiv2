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
	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/infra/onvif"
	"github.com/mysayasan/kopiv2/infra/rtsp"
	"github.com/mysayasan/kopiv2/infra/stream"
)

type onvifApi struct {
	serv          services.IOnvifDeviceService
	settings      services.IRuntimeSettingsService
	streamManager *stream.Manager
}

type discoverRequest struct {
	TimeoutMs int64 `json:"timeoutMs"`
}

type probeRequest struct {
	Address string `json:"address"`
}

type streamURIRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type cameraPasswordRequest struct {
	CurrentUsername string `json:"currentUsername"`
	CurrentPassword string `json:"currentPassword"`
	TargetUsername  string `json:"targetUsername"`
	NewPassword     string `json:"newPassword"`
	UserLevel       string `json:"userLevel"`
}

type ptzMoveRequest struct {
	Direction  string  `json:"direction"`
	Speed      float64 `json:"speed"`
	DurationMs int64   `json:"durationMs"`
}

type saveDiscoveredRequest struct {
	onvif.Device
	Description string `json:"description"`
}

type webRTCOfferRequest struct {
	Type string `json:"type"`
	SDP  string `json:"sdp"`
}

type onvifDeviceResponse struct {
	entities.OnvifDevice
	HasPassword bool `json:"hasPassword"`
}

// NewOnvifApi registers standalone ONVIF device routes.
func NewOnvifApi(router *mux.Router, serv services.IOnvifDeviceService, settings services.IRuntimeSettingsService, streamManager *stream.Manager) {
	handler := &onvifApi{serv: serv, settings: settings, streamManager: streamManager}
	group := router.PathPrefix("/onvif").Subrouter()

	group.HandleFunc("/stream-config", handler.getStreamConfig).Methods("GET")
	group.HandleFunc("/discover", handler.discover).Methods("POST")
	group.HandleFunc("/probe", handler.probe).Methods("POST")
	group.HandleFunc("/devices", handler.get).Methods("GET")
	group.HandleFunc("/devices", handler.save).Methods("POST")
	group.HandleFunc("/devices/discovered", handler.saveDiscovered).Methods("POST")
	group.HandleFunc("/devices/{id}/credentials", handler.saveCredentials).Methods("POST")
	group.HandleFunc("/devices/{id}/camera-password", handler.changeCameraPassword).Methods("POST")
	group.HandleFunc("/devices/{id}/stream-uri", handler.resolveStream).Methods("POST")
	group.HandleFunc("/devices/{id}/rtsp-test", handler.testStream).Methods("POST")
	group.HandleFunc("/devices/{id}/live-view", handler.resolveLiveView).Methods("POST")
	group.HandleFunc("/devices/{id}/ptz/move", handler.ptzMove).Methods("POST")
	group.HandleFunc("/devices/{id}/ptz/stop", handler.ptzStop).Methods("POST")
	group.HandleFunc("/devices/{id}/webrtc/offer", handler.createWebRTCAnswer).Methods("POST")
	group.HandleFunc("/devices/{id}/live.mjpeg", handler.liveMJPEG).Methods("GET")
	group.HandleFunc("/devices/{id}", handler.delete).Methods("DELETE")
}

func (a *onvifApi) getStreamConfig(w http.ResponseWriter, r *http.Request) {
	settings, err := a.settings.Stream(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, settings, "succeed")
}

func (a *onvifApi) discover(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1048576)
	req := discoverRequest{TimeoutMs: 3000}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	res, err := a.serv.Discover(r.Context(), req.TimeoutMs)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, res, "succeed")
}

func (a *onvifApi) probe(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1048576)
	var req probeRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}

	res, err := a.serv.Probe(r.Context(), req.Address)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, res, "succeed")
}

func (a *onvifApi) get(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.ParseUint(r.URL.Query().Get("limit"), 10, 64)
	offset, _ := strconv.ParseUint(r.URL.Query().Get("offset"), 10, 64)
	res, totalCnt, err := a.serv.Get(r.Context(), limit, offset)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendPagingResult(w, onvifDeviceResponses(res), limit, offset, totalCnt)
}

func (a *onvifApi) save(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1048576)
	var body entities.OnvifDevice
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}

	res, err := a.serv.Save(r.Context(), body)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, res, "succeed")
}

func (a *onvifApi) saveDiscovered(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1048576)
	var body saveDiscoveredRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}

	res, err := a.serv.Save(r.Context(), entities.OnvifDevice{
		Name:            body.Name,
		Description:     body.Description,
		Host:            body.Host,
		Port:            body.Port,
		XAddr:           body.XAddr,
		Types:           strings.Join(body.Types, " "),
		Scopes:          strings.Join(body.Scopes, " "),
		HardwareID:      body.HardwareID,
		Manufacturer:    body.Manufacturer,
		Model:           body.Model,
		FirmwareVersion: body.FirmwareVersion,
		SerialNumber:    body.SerialNumber,
		MediaXAddr:      body.MediaXAddr,
		PTZXAddr:        body.PTZXAddr,
		PTZSupported:    body.PTZSupported,
		ProfileToken:    body.ProfileToken,
		RTSPUrl:         body.RTSPURL,
		SnapshotURI:     body.SnapshotURI,
		LastSeenAt:      body.LastSeenAt,
		IsActive:        true,
	})
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, res, "succeed")
}

func (a *onvifApi) saveCredentials(w http.ResponseWriter, r *http.Request) {
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
	controllers.SendResult(w, onvifDeviceResponseFromPointer(res), "succeed")
}

func (a *onvifApi) changeCameraPassword(w http.ResponseWriter, r *http.Request) {
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
	controllers.SendResult(w, onvifDeviceResponseFromPointer(res), "succeed")
}

func (a *onvifApi) resolveStream(w http.ResponseWriter, r *http.Request) {
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

	res, err := a.serv.ResolveStream(r.Context(), id, onvif.Credentials{
		Username: body.Username,
		Password: body.Password,
	})
	if err != nil {
		sendONVIFBadRequest(w, err)
		return
	}
	controllers.SendResult(w, onvifDeviceResponseFromPointer(res), "succeed")
}

func (a *onvifApi) testStream(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	id, _ := strconv.ParseUint(params["id"], 10, 64)
	res, err := a.serv.TestStream(r.Context(), id)
	if err != nil {
		sendONVIFBadRequest(w, err)
		return
	}
	controllers.SendResult(w, res, "succeed")
}

func (a *onvifApi) resolveLiveView(w http.ResponseWriter, r *http.Request) {
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
		sendONVIFBadRequest(w, err)
		return
	}
	controllers.SendResult(w, onvifDeviceResponseFromPointer(res), "succeed")
}

func (a *onvifApi) ptzMove(w http.ResponseWriter, r *http.Request) {
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
	controllers.SendResult(w, onvifDeviceResponseFromPointer(res), "succeed")
}

func (a *onvifApi) ptzStop(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	id, _ := strconv.ParseUint(params["id"], 10, 64)
	res, err := a.serv.PTZStop(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, onvifDeviceResponseFromPointer(res), "succeed")
}

func onvifDeviceResponses(devices []*entities.OnvifDevice) []onvifDeviceResponse {
	res := make([]onvifDeviceResponse, 0, len(devices))
	for _, device := range devices {
		if device == nil {
			continue
		}
		res = append(res, onvifDeviceResponseFromPointer(device))
	}
	return res
}

func onvifDeviceResponseFromPointer(device *entities.OnvifDevice) onvifDeviceResponse {
	if device == nil {
		return onvifDeviceResponse{}
	}
	return onvifDeviceResponse{
		OnvifDevice: *device,
		HasPassword: strings.TrimSpace(device.Password) != "",
	}
}

func (a *onvifApi) createWebRTCAnswer(w http.ResponseWriter, r *http.Request) {
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

	source, err := a.serv.SnapshotSource(r.Context(), id)
	if err != nil {
		sendONVIFBadRequest(w, err)
		return
	}
	if strings.TrimSpace(source.RTSPURI) == "" {
		controllers.SendError(w, controllers.ErrBadRequest, "rtspUrl is required; resolve live view first")
		return
	}

	answer, err := a.streamManager.CreateWebRTCAnswerWithOptions(r.Context(), stream.Source{
		ID:  fmt.Sprintf("onvif-%d", id),
		URI: source.RTSPURI,
	}, stream.SessionDescription{
		Type: body.Type,
		SDP:  body.SDP,
	}, stream.Options{
		ICEServers: streamSettings.WebRTC.ICEServers,
	})
	if err != nil {
		sendONVIFBadRequest(w, err)
		return
	}
	controllers.SendResult(w, answer, "succeed")
}

func (a *onvifApi) liveMJPEG(w http.ResponseWriter, r *http.Request) {
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
		sendONVIFBadRequest(w, err)
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

	useSnapshot := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("source")), "snapshot")
	if !useSnapshot && strings.TrimSpace(source.RTSPURI) != "" {
		ffmpegPath, err := rtsp.ResolveFFmpegPath(runtimeSettings.Decoder.MJPEG.FFmpegPath)
		if err != nil {
			sendONVIFBadRequest(w, err)
			return
		}
		setMJPEGHeaders(w)
		if err := rtsp.StreamMJPEG(r.Context(), w, source.RTSPURI, rtsp.MJPEGOptions{FFmpegPath: ffmpegPath, FPS: fps, MaxWidth: maxWidth, RTSPTransport: "tcp"}); err != nil {
			return
		}
		return
	}

	if strings.TrimSpace(source.URI) != "" {
		client := &http.Client{Timeout: 2 * time.Second}
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

func sendONVIFBadRequest(w http.ResponseWriter, err error) {
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

func (a *onvifApi) delete(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	id, _ := strconv.ParseUint(params["id"], 10, 64)
	res, err := a.serv.Delete(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, res, "succeed")
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

func setMJPEGHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=mymatasan")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Connection", "close")
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
