package apis

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

type trainingApi struct {
	serv services.ITrainingService
}

// NewTrainingApi registers custom-model training dataset + image routes.
func NewTrainingApi(router *mux.Router, serv services.ITrainingService) {
	handler := &trainingApi{serv: serv}
	group := router.PathPrefix("/training").Subrouter()

	group.HandleFunc("/datasets", handler.listDatasets).Methods("GET")
	group.HandleFunc("/datasets", handler.saveDataset).Methods("POST")
	group.HandleFunc("/datasets/{id}", handler.getDataset).Methods("GET")
	group.HandleFunc("/datasets/{id}", handler.deleteDataset).Methods("DELETE")
	group.HandleFunc("/datasets/{id}/images", handler.listImages).Methods("GET")
	group.HandleFunc("/datasets/{id}/images/upload", handler.uploadImage).Methods("POST")
	group.HandleFunc("/datasets/{id}/images/from-alert", handler.addFromAlert).Methods("POST")
	group.HandleFunc("/images/{id}", handler.deleteImage).Methods("DELETE")
	group.HandleFunc("/images/{id}/file", handler.getImageFile).Methods("GET")
	group.HandleFunc("/images/{id}/annotations", handler.saveAnnotations).Methods("PUT")
	group.HandleFunc("/images/{id}/autolabel", handler.autoLabel).Methods("POST")
	group.HandleFunc("/datasets/{id}/export", handler.exportDataset).Methods("GET")
	group.HandleFunc("/models", handler.listModels).Methods("GET")
	group.HandleFunc("/models", handler.startTraining).Methods("POST")
	group.HandleFunc("/capability", handler.capability).Methods("GET")
	group.HandleFunc("/setup-deps", handler.setupDepsStatus).Methods("GET")
	group.HandleFunc("/setup-deps", handler.startSetupDeps).Methods("POST")
	group.HandleFunc("/stock-model", handler.getStockModel).Methods("GET")
	group.HandleFunc("/stock-model", handler.setStockModel).Methods("POST")
	group.HandleFunc("/lpr-model", handler.getLPRModel).Methods("GET")
	group.HandleFunc("/lpr-model", handler.setLPRModel).Methods("POST")
	group.HandleFunc("/lpr-model/import", handler.importLPRModel).Methods("POST")
	group.HandleFunc("/lpr-model/deactivate", handler.deactivateLPRModel).Methods("POST")
	group.HandleFunc("/lpr-model/install-deps", handler.installLPRDeps).Methods("POST")
	group.HandleFunc("/models/import", handler.importModel).Methods("POST")
	group.HandleFunc("/models/{id}", handler.deleteModel).Methods("DELETE")
	group.HandleFunc("/models/deactivate", handler.deactivateModel).Methods("POST")
	group.HandleFunc("/models/{id}/activate", handler.activateModel).Methods("POST")
}

func (a *trainingApi) exportDataset(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	zipPath, err := a.serv.ExportZip(r.Context(), int64(id))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	data, err := os.ReadFile(zipPath)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"dataset-%d.zip\"", id))
	w.Write(data)
}

func (a *trainingApi) capability(w http.ResponseWriter, r *http.Request) {
	controllers.SendResult(w, a.serv.MachineCapability(r.Context()), "succeed")
}

func (a *trainingApi) setupDepsStatus(w http.ResponseWriter, r *http.Request) {
	controllers.SendResult(w, a.serv.DepsSetupStatus(), "succeed")
}

func (a *trainingApi) startSetupDeps(w http.ResponseWriter, r *http.Request) {
	if err := a.serv.StartDepsSetup(r.Context()); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, a.serv.DepsSetupStatus(), "succeed")
}

func (a *trainingApi) installLPRDeps(w http.ResponseWriter, r *http.Request) {
	if err := a.serv.StartLPRDepsSetup(r.Context()); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, a.serv.DepsSetupStatus(), "succeed")
}

func (a *trainingApi) getStockModel(w http.ResponseWriter, r *http.Request) {
	controllers.SendResult(w, a.serv.GetStockModel(r.Context()), "succeed")
}

func (a *trainingApi) setStockModel(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var body struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	if err := a.serv.SetStockModel(r.Context(), body.Model, localUserID(r)); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, a.serv.GetStockModel(r.Context()), "succeed")
}

func (a *trainingApi) getLPRModel(w http.ResponseWriter, r *http.Request) {
	controllers.SendResult(w, a.serv.GetLPRModel(r.Context()), "succeed")
}

func (a *trainingApi) setLPRModel(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var body struct {
		// Model is a catalog name, an https URL, a local path, or "" / "none".
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	if err := a.serv.SetLPRModel(r.Context(), body.Model, localUserID(r)); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, a.serv.GetLPRModel(r.Context()), "succeed")
}

func (a *trainingApi) importLPRModel(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 512*1024*1024)
	if err := r.ParseMultipartForm(512 * 1024 * 1024); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, "missing weights file")
		return
	}
	defer file.Close()
	weights, err := io.ReadAll(file)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	info, err := a.serv.ImportLPRModel(r.Context(), r.FormValue("name"), weights, localUserID(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, info, "succeed")
}

func (a *trainingApi) deactivateLPRModel(w http.ResponseWriter, r *http.Request) {
	if err := a.serv.DeactivateLPRModel(r.Context(), localUserID(r)); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, a.serv.GetLPRModel(r.Context()), "succeed")
}

func (a *trainingApi) startTraining(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var body services.StartTrainingRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	model, err := a.serv.StartTraining(r.Context(), body, localUserID(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, model, "succeed")
}

func (a *trainingApi) listModels(w http.ResponseWriter, r *http.Request) {
	items, err := a.serv.ListModels(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"items": items}, "succeed")
}

func (a *trainingApi) importModel(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 512*1024*1024)
	if err := r.ParseMultipartForm(512 * 1024 * 1024); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, "missing weights file")
		return
	}
	defer file.Close()
	weights, err := io.ReadAll(file)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	req := services.ImportModelRequest{
		Name:      r.FormValue("name"),
		BaseModel: r.FormValue("baseModel"),
		Classes:   splitCSV(r.FormValue("classes")),
	}
	model, err := a.serv.ImportModel(r.Context(), req, weights, localUserID(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, model, "succeed")
}

func (a *trainingApi) activateModel(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	model, err := a.serv.ActivateModel(r.Context(), id, localUserID(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, model, "succeed")
}

func (a *trainingApi) deactivateModel(w http.ResponseWriter, r *http.Request) {
	if err := a.serv.DeactivateModel(r.Context(), localUserID(r)); err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]bool{"deactivated": true}, "succeed")
}

func (a *trainingApi) deleteModel(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	count, err := a.serv.DeleteModel(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]uint64{"deleted": count}, "succeed")
}

// splitCSV splits a comma-separated list into trimmed, non-empty values.
func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (a *trainingApi) listDatasets(w http.ResponseWriter, r *http.Request) {
	items, err := a.serv.ListDatasets(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"items": items}, "succeed")
}

func (a *trainingApi) saveDataset(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1*1024*1024)
	var body services.TrainingDatasetRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	dataset, err := a.serv.SaveDataset(r.Context(), body, localUserID(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, dataset, "succeed")
}

func (a *trainingApi) getDataset(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	dataset, err := a.serv.GetDataset(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}
	controllers.SendResult(w, dataset, "succeed")
}

func (a *trainingApi) deleteDataset(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	count, err := a.serv.DeleteDataset(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]uint64{"deleted": count}, "succeed")
}

func (a *trainingApi) listImages(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	items, err := a.serv.ListImages(r.Context(), int64(id))
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"items": items}, "succeed")
}

func (a *trainingApi) uploadImage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024*1024)
	if err := r.ParseMultipartForm(16 * 1024 * 1024); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, "missing file field")
		return
	}
	defer file.Close()
	data := make([]byte, 0, 1<<20)
	buf := make([]byte, 32*1024)
	for {
		n, readErr := file.Read(buf)
		if n > 0 {
			data = append(data, buf[:n]...)
		}
		if readErr != nil {
			break
		}
	}
	image, err := a.serv.StoreUpload(r.Context(), int64(id), data, localUserID(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, image, "succeed")
}

func (a *trainingApi) addFromAlert(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var body struct {
		AlertId int64 `json:"alertId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	image, err := a.serv.AddFromAlert(r.Context(), int64(id), body.AlertId, localUserID(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, image, "succeed")
}

func (a *trainingApi) saveAnnotations(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2*1024*1024)
	var body struct {
		Annotations []services.TrainingAnnotation `json:"annotations"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	image, err := a.serv.SaveAnnotations(r.Context(), int64(id), body.Annotations, localUserID(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, image, "succeed")
}

func (a *trainingApi) autoLabel(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	image, err := a.serv.AutoLabel(r.Context(), int64(id), localUserID(r))
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, image, "succeed")
}

func (a *trainingApi) deleteImage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	count, err := a.serv.DeleteImage(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	controllers.SendResult(w, map[string]uint64{"deleted": count}, "succeed")
}

func (a *trainingApi) getImageFile(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	data, err := a.serv.GetImageBytes(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "max-age=86400, immutable")
	w.Write(data)
}

// pathID reads a positive {id} path variable, writing a 400 on failure.
func pathID(w http.ResponseWriter, r *http.Request) (uint64, bool) {
	id, err := strconv.ParseUint(mux.Vars(r)["id"], 10, 64)
	if err != nil || id == 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}
