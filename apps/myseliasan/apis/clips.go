package apis

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myseliasan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

type clipsApi struct {
	clips services.IClipArchiveService
	audit services.IAuditService
}

// NewClipsApi mounts the fleet's critical-clip archive:
//
//	GET /api/clips?limit=&offset=&nodeId=&state=  — what the fleet has kept
//	GET /api/clips/stats                          — counts + how full the archive is
//	GET /api/clips/{id}                           — one record
//	GET /api/clips/{id}/media                     — the footage (Range-capable)
//	GET /api/clips/{id}/snapshot                  — the still image
//
// Read-only by design. There is deliberately no delete route: the whole point of this
// archive is that the footage survives things that happen at the other end, and an
// operator who can delete a clip from here can undo that from a browser. Retention
// removes media on a schedule (services/clip_archive.go), and the record of the event
// outlives the media either way.
//
// Reads follow the permission matrix like every other control-plane surface — these are
// recordings of somebody's premises, held on a second machine. Every media read is
// audited: "who watched this footage" is the question a tender and a GDPR Article 30
// record both ask, and the node's own audit trail cannot answer it once the fleet holds
// a copy.
func NewClipsApi(router *mux.Router, auth middlewares.AuthMidware, session *middlewares.AccessSessionMidware, clips services.IClipArchiveService, audit services.IAuditService) {
	h := &clipsApi{clips: clips, audit: audit}
	g := router.PathPrefix("/clips").Subrouter()
	g.Use(auth.Middleware)
	g.Use(session.Middleware)
	g.HandleFunc("", h.list).Methods("GET")
	// Literal segments before "/{id}", so they are not swallowed by the id route.
	g.HandleFunc("/stats", h.stats).Methods("GET")
	g.HandleFunc("/{id}", h.get).Methods("GET")
	g.HandleFunc("/{id}/media", h.media).Methods("GET")
	g.HandleFunc("/{id}/snapshot", h.snapshot).Methods("GET")
}

func (a *clipsApi) list(w http.ResponseWriter, r *http.Request) {
	limit, offset := clipPaging(r)
	items, total, err := a.clips.List(r.Context(), limit, offset,
		r.URL.Query().Get("nodeId"), r.URL.Query().Get("state"))
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, map[string]any{"items": items, "total": total}, "succeed")
}

func (a *clipsApi) stats(w http.ResponseWriter, r *http.Request) {
	stats, err := a.clips.Stats(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, stats, "succeed")
}

func (a *clipsApi) get(w http.ResponseWriter, r *http.Request) {
	id, ok := clipID(w, r)
	if !ok {
		return
	}
	clip, err := a.clips.Get(r.Context(), id)
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	if clip == nil {
		controllers.SendError(w, controllers.ErrBadRequest, "no such archived clip")
		return
	}
	controllers.SendResult(w, clip, "succeed")
}

func (a *clipsApi) media(w http.ResponseWriter, r *http.Request)    { a.serve(w, r, false) }
func (a *clipsApi) snapshot(w http.ResponseWriter, r *http.Request) { a.serve(w, r, true) }

// serve streams archived media. http.ServeContent does the Range handling, so a browser
// can scrub an archived clip exactly as it scrubs one on the node.
func (a *clipsApi) serve(w http.ResponseWriter, r *http.Request, snapshot bool) {
	id, ok := clipID(w, r)
	if !ok {
		return
	}
	clip, err := a.clips.Get(r.Context(), id)
	if err != nil || clip == nil {
		controllers.SendError(w, controllers.ErrBadRequest, "no such archived clip")
		return
	}

	reader, cleanup, contentType, err := a.clips.OpenMedia(r.Context(), id, snapshot)
	if err != nil {
		if errors.Is(err, services.ErrClipUnavailable) {
			// Distinguished from "no such clip": the record exists and says what happened
			// to the media. An operator looking for footage needs to know whether it was
			// never captured, has aged out, or is still being fetched.
			//
			// And distinguished per KIND, because the clip and the snapshot fail
			// independently: an alert raised without an image has no snapshot to archive
			// while its footage is perfectly fine, and reporting that as "the clip is not
			// available (state: stored)" reads like a contradiction and sends the reader
			// looking for a bug that is not there.
			if snapshot {
				controllers.SendError(w, controllers.ErrBadRequest,
					fmt.Sprintf("no snapshot was archived for this clip — the alert had no image (the footage itself is %s)", clip.State))
				return
			}
			controllers.SendError(w, controllers.ErrBadRequest,
				fmt.Sprintf("that clip's footage is not available (state: %s)", clip.State))
			return
		}
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	defer cleanup()

	// Audited BEFORE serving, and only on the opening (unranged) request of a playback:
	// a scrubbing <video> element issues dozens of ranged requests for one viewing, and
	// a row per range buries the trail this is meant to provide. Same split the node's
	// own recording download uses.
	if r.Header.Get("Range") == "" && a.audit != nil {
		kind := "clip"
		if snapshot {
			kind = "snapshot"
		}
		actorID, actorEmail, actorRole := auditActor(r)
		a.audit.Record(r.Context(), services.AuditEntry{
			Action:     "clip.view",
			ActorId:    actorID,
			ActorEmail: actorEmail,
			ActorRole:  actorRole,
			TargetType: "archived_clip",
			TargetId:   strconv.FormatInt(clip.Id, 10),
			Outcome:    "success",
			Detail: fmt.Sprintf("viewed archived %s: %s on %s (%s), event %s", kind,
				clip.Title, clip.NodeName, clip.CameraName,
				time.Unix(clip.EventAt, 0).UTC().Format(time.RFC3339)),
			ClientIp:  clientIP(r),
			UserAgent: r.UserAgent(),
			Metadata: map[string]any{
				"nodeId": clip.NodeId, "alertId": clip.AlertId, "sha256": clip.Sha256,
			},
		})
	}

	name := fmt.Sprintf("clip-%d.mp4", clip.Id)
	if snapshot {
		name = fmt.Sprintf("clip-%d.jpg", clip.Id)
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", `inline; filename="`+name+`"`)
	w.Header().Set("Accept-Ranges", "bytes")
	// The digest this control plane computed over the bytes as they arrived from the
	// node, so a recipient can verify the copy without trusting the transfer.
	if clip.Sha256 != "" && !snapshot {
		w.Header().Set("X-Clip-Sha256", clip.Sha256)
	}
	http.ServeContent(w, r, name, time.Unix(clip.UpdatedAt, 0), reader)
}

func clipID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil || id <= 0 {
		controllers.SendError(w, controllers.ErrBadRequest, "invalid clip id")
		return 0, false
	}
	return id, true
}

func clipPaging(r *http.Request) (uint64, uint64) {
	limit, _ := strconv.ParseUint(r.URL.Query().Get("limit"), 10, 64)
	offset, _ := strconv.ParseUint(r.URL.Query().Get("offset"), 10, 64)
	if limit == 0 {
		limit = 100
	}
	return limit, offset
}
