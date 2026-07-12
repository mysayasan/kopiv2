package apis

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myseliasan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
	"github.com/mysayasan/kopiv2/infra/control"
)

// recordingStreamChunk bounds each tunneled byte range so a single control-channel
// message stays well under its cap while still letting <video> stream and seek a clip
// far larger than that cap.
const recordingStreamChunk = 8 << 20 // 8 MiB

type recordingStreamApi struct {
	sender  services.ControlSender
	access  services.INodeAccessService
	session *middlewares.AccessSessionMidware
}

// NewRecordingStreamApi registers a range-capable recording player over the control
// tunnel:
//
//	GET /api/nodes/{id}/recording-stream/{segId}
//
// The whole clip can't fit in one tunneled message, so this caps each browser Range to a
// bounded chunk, fetches just that chunk from the node's segment download (which serves
// Range via http.ServeContent), and returns 206 Partial Content. The browser then walks
// the file range-by-range — enabling playback + seeking without ever exceeding the cap.
func NewRecordingStreamApi(router *mux.Router, auth middlewares.AuthMidware, sender services.ControlSender, access services.INodeAccessService, session *middlewares.AccessSessionMidware) {
	h := &recordingStreamApi{sender: sender, access: access, session: session}
	g := router.PathPrefix("/nodes").Subrouter()
	g.Use(auth.Middleware)
	g.HandleFunc("/{id}/recording-stream/{segId}", h.stream).Methods("GET")
}

func (a *recordingStreamApi) stream(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	nodeID := vars["id"]
	segID := vars["segId"]

	// Authorize exactly like the command proxy: the operator's live role must have access
	// to this node.
	roleId, actor := operatorIdentity(r)
	if a.session != nil {
		if p, perr := a.session.CurrentPrincipal(r); perr == nil && p != nil {
			roleId = p.RoleId
		}
	}
	acc, err := a.access.Resolve(r.Context(), roleId, nodeID)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	if acc.Role() == "" {
		controllers.SendError(w, controllers.ErrLimitedAccess, "no access to this node")
		return
	}

	// Parse the browser Range (default the first chunk) and cap the length.
	start, end := parseByteRange(r.Header.Get("Range"))
	if end < 0 || end-start+1 > recordingStreamChunk {
		end = start + recordingStreamChunk - 1
	}

	// Forward the transcode hint (the browser can't decode HEVC → node serves H.264).
	path := "/api/recording/segments/" + segID + "/download"
	if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("transcode")), "h264") {
		path += "?transcode=h264"
	}

	resp, err := a.sender.SendRequest(r.Context(), nodeID, control.Request{
		Method: http.MethodGet,
		Path:   path,
		Role:   acc.Role(),
		Actor:  actor,
		Headers: map[string]string{
			"Range": fmt.Sprintf("bytes=%d-%d", start, end),
		},
	})
	if err != nil {
		controllers.SendError(w, controllers.ErrLimitedAccess, "recording not streamable over the link")
		return
	}
	// The node must honor Range (206). A 200 means an older node (no Range support) or a
	// non-seekable clip (encrypted / HEVC-transcode) — we can't chunk it, so the client
	// should fall back to opening the recording on the node.
	if resp.Status != http.StatusPartialContent {
		controllers.SendError(w, controllers.ErrLimitedAccess, "recording not streamable over the link")
		return
	}

	for _, key := range []string{"Content-Type", "Content-Range", "Accept-Ranges"} {
		if v := resp.Headers[key]; v != "" {
			w.Header().Set(key, v)
		}
	}
	if w.Header().Get("Accept-Ranges") == "" {
		w.Header().Set("Accept-Ranges", "bytes")
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(resp.Body)))
	w.WriteHeader(http.StatusPartialContent)
	_, _ = w.Write(resp.Body)
}

// parseByteRange parses a single "bytes=start-end" header. end is -1 for an open range.
// Absent/invalid ranges default to 0-.
func parseByteRange(h string) (int64, int64) {
	h = strings.TrimSpace(h)
	if !strings.HasPrefix(h, "bytes=") {
		return 0, -1
	}
	spec := strings.TrimPrefix(h, "bytes=")
	if i := strings.IndexByte(spec, ','); i >= 0 {
		spec = spec[:i]
	}
	dash := strings.IndexByte(spec, '-')
	if dash < 0 {
		return 0, -1
	}
	start, _ := strconv.ParseInt(strings.TrimSpace(spec[:dash]), 10, 64)
	if start < 0 {
		start = 0
	}
	endStr := strings.TrimSpace(spec[dash+1:])
	if endStr == "" {
		return start, -1
	}
	end, _ := strconv.ParseInt(endStr, 10, 64)
	return start, end
}
