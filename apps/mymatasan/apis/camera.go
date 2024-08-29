package apis

import (
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

// CameraApi struct
type cameraApi struct {
	auth middlewares.AuthMidware
	rbac middlewares.RbacMidware
	serv services.ICameraService
}

// Create CameraApi
func NewCameraApi(
	router *mux.Router,
	auth middlewares.AuthMidware,
	rbac middlewares.RbacMidware,
	serv services.ICameraService) {
	handler := &cameraApi{
		auth: auth,
		rbac: rbac,
		serv: serv,
	}

	router.HandleFunc("/camera", handler.getCamera).Methods("GET")
}

func (m *cameraApi) getCamera(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("got / request\n")
	r.Body.Close() // We don't care about the user's data

	mw := multipart.NewWriter(w)
	defer mw.Close()

	// ctx := context.Background()

	// We don't use FormDataContentType since we want a multipart/x-mixed-replace.
	w.Header().Add("Content-Type", fmt.Sprintf(
		"multipart/x-mixed-replace;boundary=%s",
		mw.Boundary(),
	))
	w.WriteHeader(200)

	// create channel
	vidStream := make(chan []byte)

	//chanErr := make(chan error)
	go m.serv.GetMjpegStream(r.Context(), vidStream)

	// if err := <-chanErr; err != nil {
	// 	return
	// }

	for {
		v, ok := <-vidStream
		if !ok {
			break
		}
		w, _ := mw.CreatePart(textproto.MIMEHeader{
			"Content-Type": []string{"image/jpeg"},
		})
		w.Write(v)
	}
}
