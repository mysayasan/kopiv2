package apis

import (
	"encoding/json"
	"fmt"
	"net/http"
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
	oAuth2Conf *login.OAuth2ConfigModel,
	auth middlewares.AuthMidware,
	userService services.IUserLoginService) {

	login.GoogleConfig(*oAuth2Conf)
	login.GithubConfig(*oAuth2Conf)

	googleLogin := login.NewGoogleLogin(*oAuth2Conf, auth)
	githubLogin := login.NewGithubLogin(*oAuth2Conf, auth)

	handler := &loginApi{
		auth:        auth,
		userService: userService,
		googleAuth:  googleLogin,
		githubAuth:  githubLogin,
	}

	// Create api sub-router
	loginGroup := router.PathPrefix("/login").Subrouter()
	callbackGroup := router.PathPrefix("/callback").Subrouter()

	loginGroup.HandleFunc("/default", handler.defaultLogin).Methods("GET")
	loginGroup.HandleFunc("/google", handler.googleLogin).Methods("GET")
	callbackGroup.HandleFunc("/google", handler.googleCallback).Methods("GET")
	loginGroup.HandleFunc("/github", handler.githubLogin).Methods("GET")
	callbackGroup.HandleFunc("/github", handler.githubCallback).Methods("GET")
}

func (m *loginApi) defaultLogin(w http.ResponseWriter, r *http.Request) {

	r.Body = http.MaxBytesReader(w, r.Body, 1048576)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	var body map[string]interface{}

	err := dec.Decode(&body)
	if err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}

	controllers.SendResult(w, "ok")
}

func (m *loginApi) googleLogin(w http.ResponseWriter, r *http.Request) {
	m.googleAuth.Login(w, r)
}

func (m *loginApi) googleCallback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	userG, err := m.googleAuth.Callback(state, code)
	if err != nil {
		controllers.SendError(w, controllers.ErrStatusUnprocessableEntity, err.Error())
		return
	}

	user, err := m.userService.GetByEmail(r.Context(), userG.Email)
	if err != nil {
		fmt.Printf("%s", err.Error())
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
			fmt.Printf("%s", err.Error())
		}

		user.Id = int64(res)
		fmt.Printf("new user id : %d", res)
	}

	b, err := json.Marshal(user)
	if err != nil {
		fmt.Println(err.Error())
	}

	fmt.Printf("%s", b)

	// Create the Claims
	claims := &models.JwtCustomClaims{
		Id:            user.Id,
		Name:          userG.Name,
		Email:         userG.Email,
		VerifiedEmail: true,
		FamilyName:    userG.FamilyName,
		Picture:       userG.Picture,
		RoleId:        user.UserRoleId,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour * 72)),
		},
	}

	t, err := m.auth.JwtToken(*claims)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}

	controllers.SendResult(w, map[string]string{"token": t})
}

func (m *loginApi) githubLogin(w http.ResponseWriter, r *http.Request) {
	m.githubAuth.Login(w, r)
}

func (m *loginApi) githubCallback(w http.ResponseWriter, r *http.Request) {
	_ = m.githubAuth.Callback("state", "code")
}
