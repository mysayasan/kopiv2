package apis

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	myidsanentities "github.com/mysayasan/kopiv2/apps/myidsan/entities"
	enumauth "github.com/mysayasan/kopiv2/domain/enums/auth"
	"github.com/mysayasan/kopiv2/domain/models"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// maxAvatarBytes caps a stored avatar's DECODED size. The client resizes to a small
// square before upload (~10–40 KB), so this is a generous safety bound, not a target.
const maxAvatarBytes = 512 * 1024

// allowedAvatarTypes is the MIME allow-list for uploaded profile pictures.
var allowedAvatarTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/webp": true,
	"image/gif":  true,
}

// profileApi is the self-service profile surface. Today it manages the caller's own
// avatar (upload / fetch / remove); it is auth-only (acts on the caller's account),
// never RBAC-gated — the same posture as change-password and /api/mfa.
type profileApi struct {
	auth    middlewares.AuthMidware
	avatars dbsql.IGenericRepo[myidsanentities.UserAvatar]
}

func NewProfileApi(router *mux.Router, auth middlewares.AuthMidware, access *middlewares.AccessSessionMidware, avatars dbsql.IGenericRepo[myidsanentities.UserAvatar]) {
	handler := &profileApi{auth: auth, avatars: avatars}

	// Self-service: the caller's OWN avatar (auth-only, never RBAC-gated).
	self := router.PathPrefix("/profile").Subrouter()
	self.Use(auth.Middleware)
	self.HandleFunc("/avatar", handler.getOwnAvatar).Methods("GET")
	self.HandleFunc("/avatar", handler.putOwnAvatar).Methods("POST")
	self.HandleFunc("/avatar", handler.deleteOwnAvatar).Methods("DELETE")

	// Admin: manage ANY user's avatar by id from the Users page. Superadmin-only, the
	// same gate as the rest of user-account management.
	admin := router.PathPrefix("/profile/avatar").Subrouter()
	admin.Use(auth.Middleware)
	admin.Use(access.Middleware)
	admin.Use(access.RequireSuperadmin)
	admin.HandleFunc("/{userId}", handler.adminGetAvatar).Methods("GET")
	admin.HandleFunc("/{userId}", handler.adminPutAvatar).Methods("POST")
	admin.HandleFunc("/{userId}", handler.adminDeleteAvatar).Methods("DELETE")
}

func (m *profileApi) claims(r *http.Request) *models.JwtCustomClaims {
	claims, _ := r.Context().Value(enumauth.Claims).(*models.JwtCustomClaims)
	return claims
}

// --- self-service handlers (own account) ----------------------------------

func (m *profileApi) getOwnAvatar(w http.ResponseWriter, r *http.Request) {
	claims := m.claims(r)
	if claims == nil {
		controllers.SendError(w, controllers.ErrPermission, "not authenticated")
		return
	}
	m.serveAvatar(w, r, claims.Id)
}

func (m *profileApi) putOwnAvatar(w http.ResponseWriter, r *http.Request) {
	claims := m.claims(r)
	if claims == nil {
		controllers.SendError(w, controllers.ErrPermission, "not authenticated")
		return
	}
	m.storeAvatar(w, r, claims.Id)
}

func (m *profileApi) deleteOwnAvatar(w http.ResponseWriter, r *http.Request) {
	claims := m.claims(r)
	if claims == nil {
		controllers.SendError(w, controllers.ErrPermission, "not authenticated")
		return
	}
	m.removeAvatar(w, r, claims.Id)
}

// --- admin handlers (any account, superadmin-gated) -----------------------

func (m *profileApi) adminGetAvatar(w http.ResponseWriter, r *http.Request) {
	if id, ok := avatarUserId(w, r); ok {
		m.serveAvatar(w, r, id)
	}
}

func (m *profileApi) adminPutAvatar(w http.ResponseWriter, r *http.Request) {
	if id, ok := avatarUserId(w, r); ok {
		m.storeAvatar(w, r, id)
	}
}

func (m *profileApi) adminDeleteAvatar(w http.ResponseWriter, r *http.Request) {
	if id, ok := avatarUserId(w, r); ok {
		m.removeAvatar(w, r, id)
	}
}

func avatarUserId(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(mux.Vars(r)["userId"], 10, 64)
	if err != nil || id <= 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid user id")
		return 0, false
	}
	return id, true
}

// --- shared avatar logic, keyed by user id --------------------------------

// serveAvatar streams a user's avatar bytes, or 404 when none is set (the SPA then
// renders an initials fallback).
func (m *profileApi) serveAvatar(w http.ResponseWriter, r *http.Request, userId int64) {
	row, err := m.avatars.GetByUnique(r.Context(), "", "ukey1", userId)
	if err != nil || row == nil || row.Id == 0 || row.DataB64 == "" {
		http.NotFound(w, r)
		return
	}
	raw, err := base64.StdEncoding.DecodeString(row.DataB64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ct := row.ContentType
	if !allowedAvatarTypes[ct] {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	// Private + revalidate so a fresh upload (with a new ?v=) is picked up immediately
	// and the image never lands in a shared cache.
	w.Header().Set("Cache-Control", "private, no-cache")
	_, _ = w.Write(raw)
}

// storeAvatar validates a data URL (produced by the client-side resize) and upserts
// the user's avatar row.
func (m *profileApi) storeAvatar(w http.ResponseWriter, r *http.Request, userId int64) {
	var body struct {
		DataUrl string `json:"dataUrl"`
	}
	// Allow up to ~1 MB of JSON (base64 inflates the byte cap by ~4/3).
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	contentType, rawB64, err := parseImageDataURL(body.DataUrl)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		return
	}
	decoded, err := base64.StdEncoding.DecodeString(rawB64)
	if err != nil {
		controllers.SendError(w, controllers.ErrBadRequest, "avatar is not valid base64")
		return
	}
	if len(decoded) == 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "avatar is empty")
		return
	}
	if len(decoded) > maxAvatarBytes {
		controllers.SendError(w, controllers.ErrBadRequest, "avatar image is too large")
		return
	}

	now := time.Now().Unix()
	existing, err := m.avatars.GetByUnique(r.Context(), "", "ukey1", userId)
	if err == nil && existing != nil && existing.Id != 0 {
		existing.ContentType = contentType
		existing.DataB64 = rawB64
		existing.UpdatedAt = now
		if _, err := m.avatars.UpdateById(r.Context(), "", *existing); err != nil {
			controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
			return
		}
	} else {
		row := myidsanentities.UserAvatar{
			UserLoginId: userId,
			ContentType: contentType,
			DataB64:     rawB64,
			UpdatedAt:   now,
		}
		if _, err := m.avatars.Create(r.Context(), "", row); err != nil {
			controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
			return
		}
	}
	controllers.SendResult(w, map[string]any{"ok": true, "version": now})
}

func (m *profileApi) removeAvatar(w http.ResponseWriter, r *http.Request, userId int64) {
	row, err := m.avatars.GetByUnique(r.Context(), "", "ukey1", userId)
	if err == nil && row != nil && row.Id != 0 {
		if _, err := m.avatars.DeleteById(r.Context(), "", uint64(row.Id)); err != nil {
			controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
			return
		}
	}
	controllers.SendResult(w, map[string]bool{"ok": true})
}

// parseImageDataURL validates and splits a `data:image/<type>;base64,<data>` URL,
// returning the MIME type and the raw base64 payload. It rejects anything that is not
// an allow-listed image data URL.
func parseImageDataURL(dataURL string) (string, string, error) {
	dataURL = strings.TrimSpace(dataURL)
	if !strings.HasPrefix(dataURL, "data:") {
		return "", "", errBadDataURL
	}
	comma := strings.IndexByte(dataURL, ',')
	if comma < 0 {
		return "", "", errBadDataURL
	}
	header := dataURL[len("data:"):comma]
	payload := dataURL[comma+1:]
	if !strings.Contains(header, ";base64") {
		return "", "", errBadDataURL
	}
	contentType := strings.ToLower(strings.TrimSpace(strings.SplitN(header, ";", 2)[0]))
	if !allowedAvatarTypes[contentType] {
		return "", "", errUnsupportedImage
	}
	return contentType, payload, nil
}

var (
	errBadDataURL       = &avatarError{"expected an image data URL"}
	errUnsupportedImage = &avatarError{"unsupported image type (use PNG, JPEG, WebP or GIF)"}
)

type avatarError struct{ msg string }

func (e *avatarError) Error() string { return e.msg }
