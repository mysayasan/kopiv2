package apis

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/domain/entities"
	"github.com/mysayasan/kopiv2/domain/models"
	"github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
	"github.com/mysayasan/kopiv2/infra/login"
)

// LoginApi struct
type loginApi struct {
	auth        middlewares.AuthMidware
	userService services.IUserLoginService
	googleAuth  *login.GoogleLogin
	githubAuth  *login.GithubLogin
}

// Create LoginApi
func NewLoginApi(
	router *mux.Router,
	oAuth2Conf *login.OAuthProvidersConfigModel,
	auth middlewares.AuthMidware,
	userService services.IUserLoginService) {
	var googleLogin *login.GoogleLogin
	var githubLogin *login.GithubLogin

	if oAuth2Conf != nil && oAuth2Conf.Google != nil {
		googleLogin = login.NewGoogleLogin(*oAuth2Conf.Google, auth)
	}
	if oAuth2Conf != nil && oAuth2Conf.GitHub != nil {
		githubLogin = login.NewGithubLogin(*oAuth2Conf.GitHub, auth)
	}

	handler := &loginApi{
		auth:        auth,
		userService: userService,
		googleAuth:  googleLogin,
		githubAuth:  githubLogin,
	}

	// Create api sub-router
	loginGroup := router.PathPrefix("/login").Subrouter()
	callbackGroup := router.PathPrefix("/callback").Subrouter()

	loginGroup.HandleFunc("/default", handler.defaultLogin).Methods("POST")
	loginGroup.HandleFunc("/default/register", handler.defaultRegister).Methods("POST")
	loginGroup.HandleFunc("/default/logout", handler.defaultLogout).Methods("POST")

	if handler.googleAuth != nil {
		loginGroup.HandleFunc("/google", handler.googleLogin).Methods("GET")
		callbackGroup.HandleFunc("/google", handler.googleCallback).Methods("GET")
	}

	if handler.githubAuth != nil {
		loginGroup.HandleFunc("/github", handler.githubLogin).Methods("GET")
		callbackGroup.HandleFunc("/github", handler.githubCallback).Methods("GET")
	}
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

func (m *loginApi) googleLogin(w http.ResponseWriter, r *http.Request) {
	if m.googleAuth == nil {
		controllers.SendError(w, controllers.ErrLimitedAccess, "google login is not configured")
		return
	}

	m.googleAuth.Login(w, r)
}

func (m *loginApi) googleCallback(w http.ResponseWriter, r *http.Request) {
	if m.googleAuth == nil {
		controllers.SendError(w, controllers.ErrLimitedAccess, "google login is not configured")
		return
	}

	userG, err := m.googleAuth.Callback(r)
	if err != nil {
		controllers.SendError(w, controllers.ErrStatusUnprocessableEntity, err.Error())
		return
	}

	user, err := m.userService.GetByEmail(r.Context(), userG.Email)
	if err != nil {
		log.Printf("google callback user lookup warning email=%s err=%v", userG.Email, err)
	}

	if user == nil {
		user = &entities.UserLogin{
			Email:      userG.Email,
			FirstName:  userG.GivenName,
			LastName:   userG.FamilyName,
			PicUrl:     userG.Picture,
			UserRoleId: 0,
			IsActive:   true,
			CreatedBy:  0,
			CreatedAt:  time.Now().Unix(),
		}

		res, err := m.userService.Create(r.Context(), *user)
		if err != nil {
			controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
			return
		}

		user.Id = int64(res)
	}

	m.issueOAuthSession(w, r, user, userG.Name, userG.Email, userG.GivenName, userG.FamilyName, userG.Picture)
}

func (m *loginApi) githubLogin(w http.ResponseWriter, r *http.Request) {
	if m.githubAuth == nil {
		controllers.SendError(w, controllers.ErrLimitedAccess, "github login is not configured")
		return
	}

	m.githubAuth.Login(w, r)
}

func (m *loginApi) githubCallback(w http.ResponseWriter, r *http.Request) {
	if m.githubAuth == nil {
		controllers.SendError(w, controllers.ErrLimitedAccess, "github login is not configured")
		return
	}

	userG, err := m.githubAuth.Callback(r)
	if err != nil {
		controllers.SendError(w, controllers.ErrStatusUnprocessableEntity, err.Error())
		return
	}
	if strings.TrimSpace(userG.Email) == "" {
		controllers.SendError(w, controllers.ErrStatusUnprocessableEntity, "github account email is not public")
		return
	}

	name := strings.TrimSpace(userG.Name)
	if name == "" {
		name = userG.Login
	}

	user, err := m.userService.GetByEmail(r.Context(), userG.Email)
	if err != nil {
		log.Printf("github callback user lookup warning email=%s err=%v", userG.Email, err)
	}

	if user == nil {
		user = &entities.UserLogin{
			Email:     userG.Email,
			FirstName: name,
			PicUrl:    userG.AvatarURL,
			IsActive:  true,
			CreatedBy: 0,
			CreatedAt: time.Now().Unix(),
		}

		res, err := m.userService.Create(r.Context(), *user)
		if err != nil {
			controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
			return
		}

		user.Id = int64(res)
	}

	m.issueOAuthSession(w, r, user, name, userG.Email, name, "", userG.AvatarURL)
}

func (m *loginApi) issueOAuthSession(w http.ResponseWriter, r *http.Request, user *entities.UserLogin, name string, email string, givenName string, familyName string, picture string) {
	if user == nil {
		controllers.SendError(w, controllers.ErrAuthFailed, "invalid user")
		return
	}

	claims := &models.JwtCustomClaims{
		Id:            user.Id,
		Name:          name,
		GivenName:     givenName,
		Email:         email,
		VerifiedEmail: true,
		FamilyName:    familyName,
		Picture:       picture,
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
