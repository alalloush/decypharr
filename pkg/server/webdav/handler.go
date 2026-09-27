package webdav

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/internal/logger"
	"github.com/sirrobot01/decypharr/internal/utils"
	"github.com/sirrobot01/decypharr/pkg/manager"
)

func init() {
	chi.RegisterMethod("PROPFIND")
	chi.RegisterMethod("PROPPATCH")
	chi.RegisterMethod("MKCOL")
	chi.RegisterMethod("COPY")
	chi.RegisterMethod("MOVE")
	chi.RegisterMethod("LOCK")
	chi.RegisterMethod("UNLOCK")
}

const (
	PROPFIND = "PROPFIND"
)

type Handler struct {
	logger  *logger.RateLimitedLogger
	manager *manager.Manager
	logins  loginCache
}

func NewHandler(mgr *manager.Manager) *Handler {
	base := logger.New("webdav")
	if cfg := config.Get(); !cfg.DisableWebDav {
		base.Info().Bool("use_auth", cfg.UseAuth).Bool("webdav_allow_delete", cfg.WebdavAllowDelete).
			Msg("WebDAV needs the UI login or API token while use_auth is on, and refuses DELETE unless webdav_allow_delete is set")
	}
	h := &Handler{
		logger:  logger.NewRateLimitedLogger(logger.WithLogger(base)),
		manager: mgr,
	}
	return h
}

func (h *Handler) readinessMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-h.manager.IsReady():
			// WebDAV is ready, proceed
			next.ServeHTTP(w, r)
		default:
			// WebDAV is still initializing
			w.Header().Set("Retry-After", "5")
			http.Error(w, "WebDAV service is initializing, please try again shortly", http.StatusServiceUnavailable)
		}
	})
}

func (h *Handler) Routes() chi.Router {
	return h.routes(h.readinessMiddleware)
}

// routes builds the WebDAV router behind the given middlewares.
func (h *Handler) routes(before ...func(http.Handler) http.Handler) chi.Router {
	r := chi.NewRouter()
	r.Use(before...)
	r.Use(h.commonMiddleware)
	r.Use(middleware.AllowContentEncoding("gzip"))
	// Always install the auth middleware; whether it actually enforces auth is
	// decided live per-request from config, so toggling use_auth takes effect
	// without rebuilding the router (no restart).
	r.Use(h.authMiddleware)

	r.HandleFunc("/", h.handleRoot)
	r.HandleFunc("/{group}", h.handleGroup)
	r.HandleFunc("/{group}/{torrent}", h.handleTorrentFolder)
	r.HandleFunc("/{group}/{torrent}/{file}", h.handleTorrentFile)
	r.HandleFunc("/stream/{group}/{torrent}/{file}", h.handleTorrentFile)
	return r
}

func (h *Handler) IsDisabled() bool {
	cfg := config.Get()
	return cfg.DisableWebDav
}

func (h *Handler) handler(current *manager.FileInfo, children []manager.FileInfo, w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "HEAD":
		h.handleHead(current, w, r)
	case "GET":
		if current == nil {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		h.handleGet(current, w, r)
	case "DELETE":
		if !config.Get().WebdavAllowDelete {
			http.Error(w, "Forbidden: WebDAV is read-only. A delete removes the torrent from the debrid provider; set webdav_allow_delete to allow it.", http.StatusForbidden)
			return
		}
		h.handleDelete(current, w, r)
	case PROPFIND:
		h.handlePropfind(current, children, w, r)
	case "OPTIONS":
		h.handleOptions(w, r)
	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
}

func (h *Handler) handleRoot(w http.ResponseWriter, r *http.Request) {
	current := h.manager.RootInfo()
	children := h.manager.GetEntries()
	h.handler(current, children, w, r)
}

func (h *Handler) handleGroup(w http.ResponseWriter, r *http.Request) {
	group := utils.PathUnescape(chi.URLParam(r, "group"))
	currentInfo, rawEntries := h.manager.GetEntryChildren(group)
	if currentInfo == nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	h.handler(currentInfo, rawEntries, w, r)

}

func (h *Handler) handleTorrentFolder(w http.ResponseWriter, r *http.Request) {
	torrent := utils.PathUnescape(chi.URLParam(r, "torrent"))

	currentInfo, children := h.manager.GetTorrentChildren(torrent)
	h.handler(currentInfo, children, w, r)
}

func (h *Handler) handleTorrentFile(w http.ResponseWriter, r *http.Request) {
	torrent := utils.PathUnescape(chi.URLParam(r, "torrent"))
	file := utils.PathUnescape(chi.URLParam(r, "file"))
	currentInfo, err := h.manager.GetTorrentFile(torrent, file)
	if err != nil || currentInfo == nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	h.handler(currentInfo, nil, w, r)
}

// commonMiddleware advertises what the server implements. It sends no CORS
// headers: WebDAV clients are not web pages, and allowing every origin let
// any page a LAN user opened list the library and delete from it. COPY and
// MOVE are never listed: the tree mirrors the debrid accounts, so there is
// nowhere to put a copy and nothing to rename, and they get 405.
func (h *Handler) commonMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("DAV", "1, 2")
		if config.Get().WebdavAllowDelete {
			w.Header().Set("Allow", "OPTIONS, GET, HEAD, PROPFIND, DELETE")
		} else {
			w.Header().Set("Allow", "OPTIONS, GET, HEAD, PROPFIND")
		}
		next.ServeHTTP(w, r)
	})
}
