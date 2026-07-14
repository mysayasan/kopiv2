package apis

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

type settingsApi struct {
	serv           services.IRuntimeSettingsService
	camera         services.ICameraService
	userServ       services.ILocalUserService
	notifServ      services.INotificationSettingsService
	healthServ     services.IHealthSettingsService
	machineHealth  services.IMachineHealthSettingsService
	machineMetrics services.IMachineMetricsProvider
	visionTool     services.VisionToolSettings
	ffmpeg         *services.FFmpegInstaller
	pyRuntime      *services.PythonInstaller
	browseRoots    []string
	// roles lets an admin see which roles exist so they can assign one. Assignment itself
	// goes through the normal user create/update, which takes a roleId.
	roles sharedservices.IAccessRoleService
}

// NewSettingsApi registers runtime settings routes. browseRoots are extra
// site-specific directories the file picker may browse (config decoder.browseRoots).
func NewSettingsApi(router *mux.Router, serv services.IRuntimeSettingsService, camera services.ICameraService, userServ services.ILocalUserService, notifServ services.INotificationSettingsService, healthServ services.IHealthSettingsService, machineHealth services.IMachineHealthSettingsService, machineMetrics services.IMachineMetricsProvider, visionTool services.VisionToolSettings, ffmpeg *services.FFmpegInstaller, pyRuntime *services.PythonInstaller, browseRoots []string, roles sharedservices.IAccessRoleService) {
	handler := &settingsApi{serv: serv, camera: camera, userServ: userServ, notifServ: notifServ, healthServ: healthServ, machineHealth: machineHealth, machineMetrics: machineMetrics, visionTool: visionTool, ffmpeg: ffmpeg, pyRuntime: pyRuntime, browseRoots: browseRoots, roles: roles}
	group := router.PathPrefix("/settings").Subrouter()

	group.HandleFunc("/runtime", handler.getRuntime).Methods("GET")
	group.HandleFunc("/runtime", handler.saveRuntime).Methods("PUT")
	group.HandleFunc("/runtime/auto-tune", handler.autoTuneRuntime).Methods("POST")
	group.HandleFunc("/decoder/status", handler.decoderStatus).Methods("GET")
	group.HandleFunc("/fs/browse", handler.browseFilesystem).Methods("GET")
	group.HandleFunc("/decoder/ffmpeg/install", handler.ffmpegInstall).Methods("POST")
	group.HandleFunc("/decoder/ffmpeg/install/status", handler.ffmpegInstallStatus).Methods("GET")
	group.HandleFunc("/runtime/capture-auto-config", handler.captureAutoConfig).Methods("POST")
	group.HandleFunc("/runtime/gpu-devices", handler.runtimeGPUDevices).Methods("GET")
	group.HandleFunc("/runtime/reset", handler.resetRuntime).Methods("POST")
	group.HandleFunc("/notification", handler.getNotification).Methods("GET")
	group.HandleFunc("/notification", handler.saveNotification).Methods("PUT")
	group.HandleFunc("/notification/destination", handler.saveNotificationDestination).Methods("PUT")
	group.HandleFunc("/notification/destination/{id}", handler.deleteNotificationDestination).Methods("DELETE")
	group.HandleFunc("/notification/retention", handler.saveNotificationRetention).Methods("PUT")
	group.HandleFunc("/notification/test", handler.testNotification).Methods("POST")
	group.HandleFunc("/health", handler.getHealth).Methods("GET")
	group.HandleFunc("/health", handler.saveHealth).Methods("PUT")
	group.HandleFunc("/machine-health", handler.getMachineHealth).Methods("GET")
	group.HandleFunc("/machine-health", handler.saveMachineHealth).Methods("PUT")
	group.HandleFunc("/machine-health/metrics", handler.getMachineMetrics).Methods("GET")
	group.HandleFunc("/vision/ai-tool/status", handler.visionAIToolStatus).Methods("GET")
	group.HandleFunc("/vision/ai-tool/install", handler.visionAIToolInstall).Methods("POST")
	group.HandleFunc("/vision/ai-runtime/status", handler.aiRuntimeStatus).Methods("GET")
	group.HandleFunc("/vision/ai-runtime/install", handler.aiRuntimeInstall).Methods("POST")
	group.HandleFunc("/vision/ai-runtime/install/status", handler.aiRuntimeInstallStatus).Methods("GET")
	group.HandleFunc("/roles", handler.listRoles).Methods("GET")
	group.HandleFunc("/users", handler.listUsers).Methods("GET")
	group.HandleFunc("/users", handler.createUser).Methods("POST")
	group.HandleFunc("/users/{id}", handler.updateUser).Methods("PUT")
	group.HandleFunc("/users/{id}/password", handler.resetUserPassword).Methods("POST")
	group.HandleFunc("/users/{id}", handler.deleteUser).Methods("DELETE")
}

func (a *settingsApi) visionAIToolStatus(w http.ResponseWriter, r *http.Request) {
	status := services.CheckVisionTool(r.Context(), a.visionTool)
	controllers.SendResult(w, status, "succeed")
}

func (a *settingsApi) visionAIToolInstall(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var body services.InstallPackagesRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	result := services.InstallPythonPackages(r.Context(), a.visionTool, body.Packages)
	controllers.SendResult(w, result, "succeed")
}

// browseFilesystem returns a single directory level for the server-side file
// picker (used to choose the ffmpeg binary). Admin-gated and read-only.
func (a *settingsApi) browseFilesystem(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	result, err := services.BrowseDirectory(r.URL.Query().Get("path"), a.browseRoots)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, result, "succeed")
}

func (a *settingsApi) getRuntime(w http.ResponseWriter, r *http.Request) {
	settings, err := a.serv.Get(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, settings, "succeed")
}

func (a *settingsApi) runtimeGPUDevices(w http.ResponseWriter, r *http.Request) {
	controllers.SendResult(w, services.DetectDecoderGPUDevices(r.Context()), "succeed")
}

func (a *settingsApi) saveRuntime(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 2*1024*1024)
	var body services.RuntimeSettings
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}

	settings, err := a.serv.Save(r.Context(), body)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, settings, "succeed")
}

func (a *settingsApi) resetRuntime(w http.ResponseWriter, r *http.Request) {
	settings, err := a.serv.Reset(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, settings, "succeed")
}

func (a *settingsApi) getNotification(w http.ResponseWriter, r *http.Request) {
	settings, err := a.notifServ.Get(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, settings, "succeed")
}

func (a *settingsApi) saveNotification(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 256*1024)
	var body services.NotificationSettings
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	settings, err := a.notifServ.Save(r.Context(), body)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, settings, "succeed")
}

// saveNotificationDestination upserts a single delivery destination without
// touching the other destinations or the retention section (read-modify-write
// against the persisted settings). Returns the saved destination + full settings.
func (a *settingsApi) saveNotificationDestination(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 256*1024)
	var body services.NotificationDestination
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	dest, settings, err := a.notifServ.SaveDestination(r.Context(), body)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"destination": dest, "settings": settings}, "succeed")
}

// deleteNotificationDestination removes one destination by id, leaving the rest
// of the settings untouched.
func (a *settingsApi) deleteNotificationDestination(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(mux.Vars(r)["id"])
	if id == "" {
		controllers.SendError(w, controllers.ErrBadRequest, "destination id is required")
		return
	}
	settings, err := a.notifServ.DeleteDestination(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, settings, "succeed")
}

// saveNotificationRetention persists only the retention section.
func (a *settingsApi) saveNotificationRetention(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	var body services.NotificationRetentionSettings
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	settings, err := a.notifServ.SaveRetention(r.Context(), body)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, settings, "succeed")
}

func (a *settingsApi) testNotification(w http.ResponseWriter, r *http.Request) {
	severity := r.URL.Query().Get("severity")
	if err := a.notifServ.Test(r.Context(), severity); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]string{"status": "dispatched"}, "succeed")
}

func (a *settingsApi) getHealth(w http.ResponseWriter, r *http.Request) {
	settings, err := a.healthServ.Get(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, settings, "succeed")
}

func (a *settingsApi) saveHealth(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var body services.HealthSettings
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	settings, err := a.healthServ.Save(r.Context(), body)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, settings, "succeed")
}

func (a *settingsApi) getMachineHealth(w http.ResponseWriter, r *http.Request) {
	settings, err := a.machineHealth.Get(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, settings, "succeed")
}

func (a *settingsApi) saveMachineHealth(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var body services.MachineHealthSettings
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	settings, err := a.machineHealth.Save(r.Context(), body)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, settings, "succeed")
}

// getMachineMetrics returns a one-shot snapshot of current host CPU/memory/disk
// usage for the live readout and "Check now" button.
func (a *settingsApi) getMachineMetrics(w http.ResponseWriter, r *http.Request) {
	if a.machineMetrics == nil {
		controllers.SendError(w, controllers.ErrInternalServerError, "machine metrics unavailable")
		return
	}
	controllers.SendResult(w, a.machineMetrics.Sample(r.Context()), "succeed")
}

// decoderStatus reports whether a usable ffmpeg is available (found/path/version),
// so the setup wizard can decide whether to offer the download.
func (a *settingsApi) decoderStatus(w http.ResponseWriter, r *http.Request) {
	if a.ffmpeg == nil {
		controllers.SendError(w, controllers.ErrInternalServerError, "ffmpeg installer unavailable")
		return
	}
	controllers.SendResult(w, a.ffmpeg.FFmpegStatus(r.Context()), "succeed")
}

// ffmpegInstall kicks off a background ffmpeg download/install and returns the initial
// status; the client polls /decoder/ffmpeg/install/status. Admin-gated (a write).
func (a *settingsApi) ffmpegInstall(w http.ResponseWriter, r *http.Request) {
	if a.ffmpeg == nil {
		controllers.SendError(w, controllers.ErrInternalServerError, "ffmpeg installer unavailable")
		return
	}
	if err := a.ffmpeg.StartInstall(r.Context()); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, a.ffmpeg.InstallStatus(), "succeed")
}

func (a *settingsApi) ffmpegInstallStatus(w http.ResponseWriter, r *http.Request) {
	if a.ffmpeg == nil {
		controllers.SendError(w, controllers.ErrInternalServerError, "ffmpeg installer unavailable")
		return
	}
	controllers.SendResult(w, a.ffmpeg.InstallStatus(), "succeed")
}

// aiRuntimeStatus reports whether the self-contained AI Python runtime (Python +
// torch + ultralytics) is installed.
func (a *settingsApi) aiRuntimeStatus(w http.ResponseWriter, r *http.Request) {
	if a.pyRuntime == nil {
		controllers.SendError(w, controllers.ErrInternalServerError, "AI runtime installer unavailable")
		return
	}
	controllers.SendResult(w, a.pyRuntime.PythonStatus(r.Context()), "succeed")
}

// aiRuntimeInstall kicks off a background download + pip install of the AI runtime
// (Python + GPU/CPU torch + ultralytics); the client polls the status endpoint.
// Admin-gated (a write).
func (a *settingsApi) aiRuntimeInstall(w http.ResponseWriter, r *http.Request) {
	if a.pyRuntime == nil {
		controllers.SendError(w, controllers.ErrInternalServerError, "AI runtime installer unavailable")
		return
	}
	if err := a.pyRuntime.StartInstall(r.Context()); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, a.pyRuntime.InstallStatus(), "succeed")
}

func (a *settingsApi) aiRuntimeInstallStatus(w http.ResponseWriter, r *http.Request) {
	if a.pyRuntime == nil {
		controllers.SendError(w, controllers.ErrInternalServerError, "AI runtime installer unavailable")
		return
	}
	controllers.SendResult(w, a.pyRuntime.InstallStatus(), "succeed")
}

func (a *settingsApi) autoTuneRuntime(w http.ResponseWriter, r *http.Request) {
	settings, err := a.serv.Get(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}

	devices, _, err := a.camera.Get(r.Context(), 1000, 0)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	env := services.DetectDecoderAutoTuneEnvironment(r.Context(), settings.Decoder.MJPEG.FFmpegPath)
	result := services.AutoTuneRuntimeSettings(settings, devices, env)
	saved, err := a.serv.Save(r.Context(), result.Settings)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	result.Applied = true
	result.Settings = saved
	controllers.SendResult(w, result, "succeed")
}

// captureAutoConfig derives Vision.Capture frame-sourcing params from detected
// hardware (GPU presence + saved camera count), saves them, and returns the
// updated runtime settings. It does not change capture behavior — Phase 0 only.
func (a *settingsApi) captureAutoConfig(w http.ResponseWriter, r *http.Request) {
	settings, err := a.serv.Get(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	devices, _, err := a.camera.Get(r.Context(), 1000, 0)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	env := services.DetectDecoderAutoTuneEnvironment(r.Context(), settings.Decoder.MJPEG.FFmpegPath)
	tuned := services.AutoConfigCaptureSettings(settings, len(devices), env)
	saved, err := a.serv.Save(r.Context(), tuned)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, saved, "succeed")
}

func (a *settingsApi) listUsers(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	limit, offset := readPaging(r)
	users, total, err := a.userServ.Get(r.Context(), limit, offset)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{
		"items": users,
		"total": total,
	}, "succeed")
}

func (a *settingsApi) createUser(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2*1024*1024)
	var body services.CreateLocalUserRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	user, err := a.userServ.Create(r.Context(), body)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, user, "succeed")
}

func (a *settingsApi) updateUser(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	id, ok := readID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2*1024*1024)
	var body services.UpdateLocalUserRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	user, err := a.userServ.Update(r.Context(), id, body)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, user, "succeed")
}

func (a *settingsApi) resetUserPassword(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	id, ok := readID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2*1024*1024)
	var body services.ResetLocalUserPasswordRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	user, err := a.userServ.ResetPassword(r.Context(), id, body.Password)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, user, "succeed")
}

func (a *settingsApi) deleteUser(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	id, ok := readID(w, r)
	if !ok {
		return
	}
	count, err := a.userServ.Delete(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]uint64{"deleted": count}, "succeed")
}

func (a *settingsApi) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	user, ok := LocalUserFromContext(r.Context())
	if !ok || !user.IsAdmin {
		controllers.SendError(w, controllers.ErrLimitedAccess, "admin user is required")
		return false
	}
	return true
}

func readPaging(r *http.Request) (uint64, uint64) {
	limit := uint64(100)
	offset := uint64(0)
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.ParseUint(raw, 10, 64); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		if parsed, err := strconv.ParseUint(raw, 10, 64); err == nil {
			offset = parsed
		}
	}
	return limit, offset
}

func readID(w http.ResponseWriter, r *http.Request) (uint64, bool) {
	id, err := strconv.ParseUint(mux.Vars(r)["id"], 10, 64)
	if err != nil || id == 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid user id")
		return 0, false
	}
	return id, true
}

// listRoles returns the roles an admin can assign. Admin-only: the role list describes the
// authorization model, and a viewer has no business enumerating it.
func (a *settingsApi) listRoles(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	roles, err := a.roles.List(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, roles, "succeed")
}
