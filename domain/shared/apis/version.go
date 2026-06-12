package apis

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/infra/versioning"
)

type versionApi struct {
	appName  string
	manifest versioning.Manifest
}

// NewVersionApi exposes the public runtime version endpoint for the selected app.
func NewVersionApi(router *mux.Router, appName string, manifest versioning.Manifest) {
	handler := &versionApi{
		appName:  appName,
		manifest: manifest,
	}

	router.HandleFunc("/version", handler.get).Methods("GET")
}

func (m *versionApi) get(w http.ResponseWriter, r *http.Request) {
	info, err := m.manifest.InfoForApp(m.appName)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}

	controllers.SendResult(w, info, "succeed")
}
