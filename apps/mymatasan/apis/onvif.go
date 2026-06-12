package apis

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/infra/discovery"
	"github.com/mysayasan/kopiv2/infra/stream"
)

type onvifApi struct {
	serv     services.ICameraService
	settings services.IRuntimeSettingsService
}

type discoverRequest struct {
	TimeoutMs int64 `json:"timeoutMs"`
}

type probeRequest struct {
	Address string `json:"address"`
}

type scanRequest struct {
	TimeoutMs int64    `json:"timeoutMs"`
	Methods   []string `json:"methods,omitempty"`
	CIDR      string   `json:"cidr,omitempty"`
}

// NewOnvifApi registers ONVIF discovery routes (no device CRUD — see NewCameraApi).
func NewOnvifApi(router *mux.Router, serv services.ICameraService, settings services.IRuntimeSettingsService, streamManager *stream.Manager) {
	handler := &onvifApi{serv: serv, settings: settings}
	group := router.PathPrefix("/onvif").Subrouter()

	group.HandleFunc("/stream-config", handler.getStreamConfig).Methods("GET")
	group.HandleFunc("/local-subnets", handler.localSubnets).Methods("GET")
	group.HandleFunc("/discover", handler.discover).Methods("POST")
	group.HandleFunc("/scan", handler.scan).Methods("POST")
	group.HandleFunc("/probe", handler.probe).Methods("POST")
}

func (a *onvifApi) getStreamConfig(w http.ResponseWriter, r *http.Request) {
	settings, err := a.settings.Stream(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, settings, "succeed")
}

func (a *onvifApi) localSubnets(w http.ResponseWriter, r *http.Request) {
	subnets := discovery.LocalSubnetCIDRs()
	controllers.SendResult(w, subnets, "succeed")
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

func (a *onvifApi) scan(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1048576)
	req := scanRequest{TimeoutMs: 5000}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	if req.TimeoutMs <= 0 {
		req.TimeoutMs = 5000
	}
	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	ctx := r.Context()

	var devices []discovery.Device
	if req.CIDR != "" {
		wantsPortScan := scanPortScanWanted(req.Methods)
		nonPortScanMethods := scanExcludePortScan(req.Methods)
		if len(nonPortScanMethods) > 0 || !wantsPortScan {
			devices = discovery.Discover(ctx, timeout, nonPortScanMethods...)
		}
		if wantsPortScan {
			extra, err := discovery.DiscoverPortScan(ctx, req.CIDR)
			if err == nil {
				devices = discovery.MergeDevices(devices, extra)
			}
		}
	} else {
		devices = discovery.Discover(ctx, timeout, req.Methods...)
	}
	controllers.SendResult(w, devices, "succeed")
}

// scanPortScanWanted reports whether port scan should run given the requested methods.
func scanPortScanWanted(methods []string) bool {
	if len(methods) == 0 {
		return true
	}
	for _, m := range methods {
		m = strings.ToLower(strings.TrimSpace(m))
		if m == "all" || m == discovery.MethodPortScan {
			return true
		}
	}
	return false
}

// scanExcludePortScan returns the methods list with port scan removed.
func scanExcludePortScan(methods []string) []string {
	if len(methods) == 0 {
		return []string{discovery.MethodSSDPUPnP, discovery.MethodMDNS, discovery.MethodSADP}
	}
	out := methods[:0:len(methods)]
	for _, m := range methods {
		if strings.ToLower(strings.TrimSpace(m)) != discovery.MethodPortScan {
			out = append(out, m)
		}
	}
	return out
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
