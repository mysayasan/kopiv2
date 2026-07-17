package apis

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myiotsan/kb"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
)

// NewKbApi serves the shipped knowledge base (setup guides for the solar/Modbus device profiles).
// It is read-only reference content compiled into the binary, so it works with no network access —
// readable by any signed-in user (the RBAC matrix grants /api/kb to viewer and operator too).
func NewKbApi(router *mux.Router) {
	g := router.PathPrefix("/kb").Subrouter()
	g.HandleFunc("", listArticles).Methods("GET")
	g.HandleFunc("/{slug}", getArticle).Methods("GET")
}

func listArticles(w http.ResponseWriter, r *http.Request) {
	controllers.SendResult(w, map[string]any{"items": kb.Articles()}, "succeed")
}

func getArticle(w http.ResponseWriter, r *http.Request) {
	slug := mux.Vars(r)["slug"]
	article, ok := kb.Get(slug)
	if !ok {
		controllers.SendError(w, controllers.ErrNotFound, "no such article")
		return
	}
	controllers.SendResult(w, map[string]any{"article": article}, "succeed")
}
