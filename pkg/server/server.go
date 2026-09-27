package server

import (
	"context"
	"crypto/rand"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/sessions"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/internal/logger"
	"github.com/sirrobot01/decypharr/pkg/manager"
	"github.com/sirrobot01/decypharr/pkg/server/qbit"
	"github.com/sirrobot01/decypharr/pkg/server/sabnzbd"
	"github.com/sirrobot01/decypharr/pkg/server/webdav"
	"github.com/sirrobot01/decypharr/pkg/stats"
)

//go:embed templates/*
var content embed.FS

//go:embed assets/build/*
var assetsEmbed embed.FS

//go:embed assets/images/*
var imagesEmbed embed.FS

type AddRequest struct {
	Url        string   `json:"url"`
	Arr        string   `json:"arr"`
	File       string   `json:"file"`
	NotSymlink bool     `json:"notSymlink"`
	Content    string   `json:"content"`
	Seasons    []string `json:"seasons"`
	Episodes   []string `json:"episodes"`
}

type ArrResponse struct {
	Name string `json:"name"`
	Url  string `json:"url"`
}

type ContentResponse struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Type  string `json:"type"`
	ArrID string `json:"arr"`
}

// shutdownGrace is how long a stop waits for in-flight requests before it
// closes their connections. A streaming read (WebDAV, STRM) stays in flight
// for as long as the player keeps reading, and a restart cannot bind the next
// listener until the old server has stopped.
const shutdownGrace = 5 * time.Second

// handlerGrace is how long a stop then waits for the handlers on the closed
// connections to return, so the caller does not reset the manager under them.
// Handlers that touch their connection fail on the next read or write; the cap
// is for one that ignores it (utils.DownloadFile takes no context) and must
// not hold a restart open indefinitely.
const handlerGrace = 30 * time.Second

// readHeaderTimeout bounds how long a client may take to send a request's
// headers, so a connection that sends nothing, or trickles its headers
// (slowloris), cannot hold a socket and a goroutine open indefinitely.
const readHeaderTimeout = 10 * time.Second

// idleTimeout closes a kept-alive connection that has not started another
// request for this long. It is longer than the 90 s after which Go's default
// HTTP client drops idle connections, so such clients close first.
const idleTimeout = 2 * time.Minute

type Server struct {
	router       *chi.Mux
	logger       zerolog.Logger
	manager      *manager.Manager
	stats        *stats.Collector
	cookie       *sessions.CookieStore
	templates    *template.Template
	nzbUserAgent string
	urlBase      string
	restartFunc  func()

	// instanceID names this run of the service. A restart builds a new Server,
	// so a client that sees it change on /version knows the new listener is up.
	instanceID string

	// inflight counts handlers that have started and not returned.
	// http.Server.Close does not wait for them; stop does.
	inflight atomic.Int64
}

// parseTemplates parses every page template.
func parseTemplates() *template.Template {
	return template.Must(template.ParseFS(
		content,
		"templates/layout.html",
		"templates/setup_layout.html",
		"templates/index.html",
		"templates/download.html",
		"templates/repair.html",
		"templates/reacquire.html",
		"templates/repair_tabs.html",
		"templates/stats.html",
		"templates/config.html",
		"templates/browse.html",
		"templates/login.html",
		"templates/register.html",
		"templates/setup.html",
	))
}

func New(mgr *manager.Manager) *Server {
	l := logger.New("http")
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.StripSlashes)
	r.Use(middleware.RedirectSlashes)

	cfg := config.Get()

	templates := parseTemplates()

	statsCollector := stats.New(mgr)

	s := &Server{
		logger:     l,
		manager:    mgr,
		stats:      statsCollector,
		cookie:     newCookieStore(cfg.SecretKey()),
		templates:  templates,
		urlBase:    cfg.URLBase,
		instanceID: rand.Text(),
	}

	qb := qbit.New(mgr)
	sb := sabnzbd.New(mgr)
	wd := webdav.NewHandler(mgr)

	routes := make(map[string]http.Handler)
	routes["/api/v2"] = qb.Routes()

	if !wd.IsDisabled() {
		routes["/webdav"] = wd.Routes()
	}
	// Serves the URLs written into .strm files; independent of DisableWebDav.
	routes["/stream"] = wd.StreamRoutes()
	routes["/sabnzbd"] = sb.Routes()

	// Trim trailing slash so chi registers the URLBase root path itself
	routePath := cfg.URLBase
	if routePath != "/" {
		routePath = strings.TrimSuffix(routePath, "/")
	}
	r.Route(routePath, func(r chi.Router) {
		// Mount web routes
		r.Mount("/", s.WebRoutes())

		for path, handler := range routes {
			r.Mount(path, handler)
		}

		r.Group(func(r chi.Router) {
			r.Use(s.authMiddleware)

			// logs
			r.Get("/logs", s.getLogs) // deprecated, use /debug/logs

			r.Route("/debug", func(r chi.Router) {
				r.Get("/stats", s.stats.Handler())
				r.Post("/speedtest", s.handleSpeedTest)
				r.Get("/logs", s.getLogs)
				r.Get("/logs/rclone", s.getRcloneLogs)
				r.Get("/ingests", s.handleIngests)
				r.Get("/ingests/{debrid}", s.handleIngestsByDebrid)
			})

			// Webhooks
			r.Post("/webhooks/tautulli", s.handleTautulli)
		})
	})
	s.router = r
	return s
}

// newCookieStore holds the browser session. saveSession sets Secure per
// response, from how the client connected.
func newCookieStore(secret string) *sessions.CookieStore {
	store := sessions.NewCookieStore([]byte(secret))
	store.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   86400 * 7,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
	return store
}

func (s *Server) SetRestartFunc(restartFunc func()) {
	s.restartFunc = restartFunc
}

func (s *Server) Restart() {
	if s.restartFunc != nil {
		time.Sleep(200 * time.Millisecond)
		s.restartFunc()
	} else {
		s.logger.Warn().Msg("Restart function not set")
	}
}

// Start serves the UI, API and WebDAV until ctx is done. It returns an error
// when the listener cannot bind (the port is taken, or the host has no such
// address), so the process exits instead of running on without HTTP.
func (s *Server) Start(ctx context.Context) error {
	cfg := config.Get()

	addr := fmt.Sprintf("%s:%s", cfg.BindAddress, cfg.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("HTTP server cannot listen on %s: %w", addr, err)
	}

	// Start background stats collector
	s.stats.Start(ctx)

	s.logger.Info().Msgf("Starting server on %s%s", addr, cfg.URLBase)
	srv := newHTTPServer(s.trackInflight(s.router), readHeaderTimeout, idleTimeout)
	served := make(chan error, 1)
	go func() { served <- srv.Serve(listener) }()

	select {
	case <-ctx.Done():
		s.logger.Info().Msg("Shutting down gracefully...")
		return s.stop(srv, shutdownGrace, handlerGrace)
	case err := <-served:
		// Serve returns on its own only when accepting connections fails for
		// good; that is the same outage as a failed bind.
		_ = s.stop(srv, shutdownGrace, handlerGrace)
		return fmt.Errorf("HTTP server stopped accepting connections: %w", err)
	}
}

// newHTTPServer bounds only the time a client takes to send its headers and
// to start its next request on a kept-alive connection. ReadTimeout and
// WriteTimeout cover whole bodies: they would cut off a slow upload, and a
// WebDAV or STRM stream that a player reads for hours.
func newHTTPServer(handler http.Handler, headerTimeout, idle time.Duration) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: headerTimeout,
		IdleTimeout:       idle,
	}
}

func (s *Server) trackInflight(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.inflight.Add(1)
		defer s.inflight.Add(-1)
		next.ServeHTTP(w, r)
	})
}

// stop shuts srv down without letting a long request hold it open: requests
// get grace to finish, then their connections are closed, and stop waits up to
// handlerGrace for their handlers to return.
func (s *Server) stop(srv *http.Server, grace, handlerGrace time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	err := srv.Shutdown(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	s.logger.Warn().Int64("requests", s.inflight.Load()).Dur("grace", grace).
		Msg("Requests still in flight after the shutdown grace period; closing them")
	err = srv.Close()
	deadline := time.Now().Add(handlerGrace)
	for s.inflight.Load() > 0 {
		if time.Now().After(deadline) {
			s.logger.Warn().Int64("handlers", s.inflight.Load()).Dur("grace", handlerGrace).
				Msg("Handlers still running after their connections were closed; continuing")
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	return err
}

func (s *Server) getLogs(w http.ResponseWriter, r *http.Request) {
	logFile := filepath.Join(logger.GetLogPath(), "decypharr.log")

	// Open and read the file
	file, err := os.Open(logFile)
	if err != nil {
		http.Error(w, "Error reading log file", http.StatusInternalServerError)
		return
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			s.logger.Error().Err(err).Msg("Error closing log file")
		}
	}(file)

	// Set headers
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", "inline; filename=application.log")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	// Stream the file
	if _, err := io.Copy(w, file); err != nil {
		http.Error(w, "Error streaming log file", http.StatusInternalServerError)
		return
	}
}

func (s *Server) getRcloneLogs(w http.ResponseWriter, r *http.Request) {
	// Rclone logs resides in the same directory as the application logs
	logFile := filepath.Join(logger.GetLogPath(), "rclone.log")
	// Open and read the file
	file, err := os.Open(logFile)
	if err != nil {
		http.Error(w, "Error reading log file", http.StatusInternalServerError)
		return
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			return
		}
	}(file)

	// Set headers
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", "inline; filename=application.log")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	// Stream the file
	if _, err := io.Copy(w, file); err != nil {
		http.Error(w, fmt.Sprintf("error stremaing file %s", err), http.StatusInternalServerError)
		return
	}
}
