package apis

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myidsan/services"
	"github.com/mysayasan/kopiv2/domain/entities"
	enumauth "github.com/mysayasan/kopiv2/domain/enums/auth"
	"github.com/mysayasan/kopiv2/domain/models"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
	"github.com/mysayasan/kopiv2/infra/login"
)

// LoginApi struct
type loginApi struct {
	auth        middlewares.AuthMidware
	userService services.IUserLoginService
	providers   *login.Registry
}

// Create LoginApi. Returns the identity-provider registry so the server-rendered
// federated login page can offer the same providers as the SPA.
func NewLoginApi(
	router *mux.Router,
	oAuth2Conf *login.OAuthProvidersConfigModel,
	auth middlewares.AuthMidware,
	userService services.IUserLoginService) *login.Registry {
	handler := &loginApi{
		auth:        auth,
		userService: userService,
		providers:   login.BuildRegistry(oAuth2Conf),
	}

	// Create api sub-router
	loginGroup := router.PathPrefix("/login").Subrouter()
	callbackGroup := router.PathPrefix("/callback").Subrouter()

	// Public: which social-login providers are actually configured, so the SPA shows
	// only the buttons that work (no dead Google/GitHub links).
	loginGroup.HandleFunc("/providers", handler.listProviders).Methods("GET")
	loginGroup.HandleFunc("/default", handler.defaultLogin).Methods("POST")
	loginGroup.HandleFunc("/default/register", handler.defaultRegister).Methods("POST")
	loginGroup.HandleFunc("/default/logout", handler.defaultLogout).Methods("POST")
	// Authenticated: change the signed-in local account's password (also clears the
	// forced first-login must-change flag).
	loginGroup.Handle("/default/change-password", auth.Middleware(http.HandlerFunc(handler.changePassword))).Methods("POST")

	// One generic pair of routes serves every registered redirect provider. The fixed
	// paths above are registered first, so mux matches them before this catch-all.
	loginGroup.HandleFunc("/{provider:[a-z][a-z0-9_.:-]*}", handler.providerLogin).Methods("GET")
	callbackGroup.HandleFunc("/{provider:[a-z][a-z0-9_.:-]*}", handler.providerCallback).Methods("GET")

	return handler.providers
}

// listProviders reports the configured federated login providers. `list` is the
// authoritative registry view (key + button label, in render order); the per-key
// booleans keep the pre-registry SPA contract (`{google:bool, github:bool}`) working
// until every deployed frontend reads `list`.
func (m *loginApi) listProviders(w http.ResponseWriter, r *http.Request) {
	type providerInfo struct {
		Key         string `json:"key"`
		DisplayName string `json:"displayName"`
	}
	resp := map[string]any{
		"google": false,
		"github": false,
	}
	list := []providerInfo{}
	for _, key := range m.providers.Keys() {
		p := m.providers.Get(key)
		if p == nil {
			continue
		}
		resp[key] = true
		list = append(list, providerInfo{Key: key, DisplayName: p.DisplayName()})
	}
	resp["list"] = list
	controllers.SendResult(w, resp)
}

func (m *loginApi) providerLogin(w http.ResponseWriter, r *http.Request) {
	key := mux.Vars(r)["provider"]
	provider := m.providers.Get(key)
	if provider == nil {
		controllers.SendError(w, controllers.ErrLimitedAccess, key+" login is not configured")
		return
	}

	// Remember where to land after the OAuth round-trip (e.g. an /api/auth/authorize
	// URL when this login was reached via a relying-app SSO redirect).
	setOAuthContinue(w, r, provider.Key(), r.URL.Query().Get("continue"))
	provider.Login(w, r)
}

func (m *loginApi) providerCallback(w http.ResponseWriter, r *http.Request) {
	key := mux.Vars(r)["provider"]
	provider := m.providers.Get(key)
	if provider == nil {
		controllers.SendError(w, controllers.ErrLimitedAccess, key+" login is not configured")
		return
	}

	identity, err := provider.Callback(r)
	if err != nil {
		controllers.SendError(w, controllers.ErrStatusUnprocessableEntity, err.Error())
		return
	}
	if strings.TrimSpace(identity.Email) == "" {
		// e.g. a GitHub account with no public email — without an email there is no
		// account to show the operator and no valid Email claim to issue.
		controllers.SendError(w, controllers.ErrStatusUnprocessableEntity, provider.DisplayName()+" account email is not available")
		return
	}

	user, err := m.userService.UpsertFederated(r.Context(), *identity)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrFederatedIdentityConflict),
			errors.Is(err, services.ErrInactiveAccount):
			controllers.SendError(w, controllers.ErrLimitedAccess, err.Error())
		case errors.Is(err, services.ErrFederatedIdentityInvalid):
			controllers.SendError(w, controllers.ErrStatusUnprocessableEntity, err.Error())
		default:
			controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		}
		return
	}

	if err := m.setOAuthSession(w, r, user, identity); err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, consumeOAuthContinue(w, r, provider.Key()), http.StatusFound)
}

func (m *loginApi) defaultLogin(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1048576)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	body := new(login.DefaultLoginRequestModel)
	err := dec.Decode(&body)
	if err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}

	user, err := m.userService.AuthenticateDefault(r.Context(), body.Username, body.Password)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrInvalidCredentialPayload):
			controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		case errors.Is(err, services.ErrInvalidCredential):
			controllers.SendError(w, controllers.ErrAuthFailed, err.Error())
		case errors.Is(err, services.ErrInactiveAccount):
			controllers.SendError(w, controllers.ErrLimitedAccess, err.Error())
		case errors.Is(err, services.ErrThirdPartyOnlyAccount):
			controllers.SendError(w, controllers.ErrLimitedAccess, err.Error())
		default:
			controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		}
		return
	}

	m.issueLocalSession(w, r, user)
}

func (m *loginApi) defaultRegister(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1048576)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	body := new(login.DefaultRegisterRequestModel)
	err := dec.Decode(&body)
	if err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}

	_, err = m.userService.RegisterLocal(r.Context(), entities.UserLogin{
		Email:      body.Username,
		Userpwd:    body.Password,
		FirstName:  body.FirstName,
		LastName:   body.LastName,
		UserRoleId: 0,
		IsActive:   true,
		CreatedBy:  0,
	})
	if err != nil {
		switch {
		case errors.Is(err, services.ErrInvalidCredentialPayload):
			controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		case errors.Is(err, services.ErrAccountAlreadyExists):
			controllers.SendError(w, controllers.ErrConflict, err.Error())
		case errors.Is(err, services.ErrThirdPartyOnlyAccount):
			controllers.SendError(w, controllers.ErrLimitedAccess, err.Error())
		default:
			controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		}
		return
	}

	user, err := m.userService.AuthenticateDefault(r.Context(), body.Username, body.Password)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}

	m.issueLocalSession(w, r, user)
}

func (m *loginApi) defaultLogout(w http.ResponseWriter, r *http.Request) {
	m.auth.ClearAuthCookies(w, r)
	controllers.SendResult(w, map[string]bool{"ok": true})
}

func (m *loginApi) changePassword(w http.ResponseWriter, r *http.Request) {
	claims, _ := r.Context().Value(enumauth.Claims).(*models.JwtCustomClaims)
	if claims == nil {
		controllers.SendError(w, controllers.ErrPermission, "not authenticated")
		return
	}
	var body struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1048576)).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}
	if err := m.userService.ChangePassword(r.Context(), claims.Id, body.CurrentPassword, body.NewPassword); err != nil {
		switch {
		case errors.Is(err, services.ErrInvalidCredential):
			controllers.SendError(w, controllers.ErrAuthFailed, "current password is incorrect")
		case errors.Is(err, services.ErrThirdPartyOnlyAccount):
			controllers.SendError(w, controllers.ErrLimitedAccess, err.Error())
		default:
			controllers.SendError(w, controllers.ErrBadRequest, err.Error())
		}
		return
	}
	controllers.SendResult(w, map[string]bool{"ok": true})
}

// setOAuthSession issues the signed-in session cookies for a federated-login user. It
// does NOT write a response body, so the caller controls the outcome (a redirect to
// the pending `continue` target, or the dashboard) instead of the browser landing on
// a raw JSON payload after the OAuth round-trip.
func (m *loginApi) setOAuthSession(w http.ResponseWriter, r *http.Request, user *entities.UserLogin, identity *login.Identity) error {
	if user == nil || identity == nil {
		return errors.New("invalid user")
	}

	name := strings.TrimSpace(identity.Name)
	if name == "" {
		name = strings.TrimSpace(strings.TrimSpace(user.FirstName) + " " + strings.TrimSpace(user.LastName))
	}
	if name == "" {
		name = user.Email
	}

	claims := &models.JwtCustomClaims{
		Id:            user.Id,
		Name:          name,
		GivenName:     identity.GivenName,
		Email:         user.Email,
		VerifiedEmail: true,
		FamilyName:    identity.FamilyName,
		Picture:       identity.Picture,
		RoleId:        user.UserRoleId,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour * 72)),
		},
	}

	return m.auth.IssueAuthCookies(w, r, *claims)
}

func oauthContinueCookieName(provider string) string {
	return "oauth_continue_" + strings.ToLower(strings.TrimSpace(provider))
}

// setOAuthContinue stores a validated post-login return path for the social-login
// round-trip, scoped to the provider's callback path so it rides back with the OAuth
// redirect. Unsafe or absent values are ignored (the callback then defaults to "/").
// The value is base64-encoded because the return path carries query characters a raw
// cookie value cannot.
func setOAuthContinue(w http.ResponseWriter, r *http.Request, provider, continueTo string) {
	continueTo = cleanContinuePath(continueTo)
	if continueTo == "/" {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     oauthContinueCookieName(provider),
		Value:    base64.RawURLEncoding.EncodeToString([]byte(continueTo)),
		Path:     "/api/callback/" + strings.ToLower(provider),
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int((5 * time.Minute).Seconds()),
	})
}

// consumeOAuthContinue reads and clears the return path set by setOAuthContinue,
// falling back to "/" when absent or invalid. Single-use: the cookie is expired here.
func consumeOAuthContinue(w http.ResponseWriter, r *http.Request, provider string) string {
	cookie, err := r.Cookie(oauthContinueCookieName(provider))
	if err != nil || cookie.Value == "" {
		return "/"
	}
	http.SetCookie(w, &http.Cookie{
		Name:     oauthContinueCookieName(provider),
		Value:    "",
		Path:     "/api/callback/" + strings.ToLower(provider),
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	raw, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil {
		return "/"
	}
	return cleanContinuePath(string(raw))
}

func (m *loginApi) issueLocalSession(w http.ResponseWriter, r *http.Request, user *entities.UserLogin) {
	if user == nil {
		controllers.SendError(w, controllers.ErrAuthFailed, "invalid username or password")
		return
	}

	name := strings.TrimSpace(strings.TrimSpace(user.FirstName) + " " + strings.TrimSpace(user.LastName))
	if name == "" {
		name = user.Email
	}

	claims := &models.JwtCustomClaims{
		Id:            user.Id,
		Name:          name,
		GivenName:     user.FirstName,
		FamilyName:    user.LastName,
		Email:         user.Email,
		VerifiedEmail: true,
		Picture:       user.PicUrl,
		RoleId:        user.UserRoleId,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour * 72)),
		},
	}

	if err := m.auth.IssueAuthCookies(w, r, *claims); err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}

	controllers.SendResult(w, map[string]bool{"ok": true})
}
