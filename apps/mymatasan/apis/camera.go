package apis

import (
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

// CameraApi struct
type cameraApi struct {
	auth    middlewares.AuthMidware
	rbac    middlewares.RbacMidware
	serv    services.ICameraStreamService
	camsess map[uint64](chan []byte)
}

// Create CameraApi
func NewCameraApi(
	router *mux.Router,
	auth middlewares.AuthMidware,
	rbac middlewares.RbacMidware,
	serv services.ICameraStreamService) {
	camsess := make(map[uint64](chan []byte))
	handler := &cameraApi{
		auth:    auth,
		rbac:    rbac,
		serv:    serv,
		camsess: camsess,
	}
	// Create api sub-router
	group := router.PathPrefix("/camera").Subrouter()
	// group.Use(auth.Middleware)

	// Stream Group Handlers
	streamGroup := group.PathPrefix("/stream").Subrouter()

	streamGroup.HandleFunc("", rbac.RbacHandler(handler.get)).Methods("GET")
	streamGroup.HandleFunc("", rbac.RbacHandler(handler.post)).Methods("GET")
	streamGroup.HandleFunc("", rbac.RbacHandler(handler.put)).Methods("PUT")
	streamGroup.HandleFunc("/{id}", rbac.RbacHandler(handler.delete)).Methods("DELETE")
	streamGroup.HandleFunc("/mjpeg/{id}", handler.getMjpegStream).Methods("GET")
}

func (m *cameraApi) getMjpegStream(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	id, _ := strconv.ParseUint(params["id"], 10, 64)

	ctx := r.Context()
	r.Body.Close()

	mw := multipart.NewWriter(w)
	defer mw.Close()

	w.Header().Add("Content-Type", fmt.Sprintf(
		"multipart/x-mixed-replace;boundary=%s",
		mw.Boundary(),
	))
	w.WriteHeader(200)

	// create channel
	if _, ok := m.camsess[id]; !ok {
		res, err := m.serv.GetById(r.Context(), id)
		if err != nil {
			controllers.SendError(w, controllers.ErrNotFound, err.Error())
			return
		}
		m.camsess[id] = make(chan []byte)
		go m.serv.ReadMjpeg(r.Context(), res.Url, m.camsess[id])
	}

	for {
		select {
		case <-ctx.Done():
			{
				// delete(m.camsess, id)
				return
			}
		case v, ok := <-m.camsess[id]:
			if !ok {
				break
			}
			w, _ := mw.CreatePart(textproto.MIMEHeader{
				"Content-Type": []string{"image/jpeg"},
			})
			w.Write(v)
		}
	}
}

func (m *cameraApi) get(w http.ResponseWriter, r *http.Request) {

	limit, _ := strconv.ParseUint(r.URL.Query().Get("limit"), 10, 64)
	offset, _ := strconv.ParseUint(r.URL.Query().Get("offset"), 10, 64)

	res, totalCnt, err := m.serv.Get(r.Context(), limit, offset)
	if err != nil {
		controllers.SendError(w, controllers.ErrNotFound, err.Error())
		return
	}

	controllers.SendPagingResult(w, res, limit, offset, totalCnt)
}

func (m *cameraApi) post(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1048576)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	body := new(entities.CameraStream)

	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}

	fmt.Printf("%v", body)

	res, err := m.serv.Create(r.Context(), *body)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}

	controllers.SendResult(w, res, "succeed")
}

func (m *cameraApi) put(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1048576)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	body := new(entities.CameraStream)

	if err := dec.Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, err.Error())
		return
	}

	fmt.Printf("%v", body)

	res, err := m.serv.Update(r.Context(), *body)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}

	controllers.SendResult(w, res, "succeed")
}

func (m *cameraApi) delete(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	id, _ := strconv.ParseUint(params["id"], 10, 64)

	res, err := m.serv.Delete(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}

	controllers.SendResult(w, res, "succeed")
}
