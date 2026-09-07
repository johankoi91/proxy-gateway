package httpapi

import (
	"crypto/subtle"
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"

	"lightnode-mac-controller/internal/controller"
	"lightnode-mac-controller/internal/model"
)

//go:embed static/* static/assets/*
var embedded embed.FS

type Server struct {
	controller  *controller.Controller
	user        string
	password    string
	disableAuth bool
	static      http.Handler
}

func New(controller *controller.Controller, user, password string, disableAuth bool) http.Handler {
	staticFS, _ := fs.Sub(embedded, "static")
	s := &Server{controller: controller, user: user, password: password, disableAuth: disableAuth, static: http.FileServer(http.FS(staticFS))}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", method(http.MethodGet, s.health))
	mux.HandleFunc("/api/snapshot", s.auth(method(http.MethodGet, s.snapshot)))
	mux.HandleFunc("/api/tests/run", s.auth(method(http.MethodPost, s.runTests)))
	mux.HandleFunc("/api/actions/restart", s.auth(method(http.MethodPost, s.restart)))
	mux.HandleFunc("/api/actions/start", s.auth(method(http.MethodPost, s.start)))
	mux.HandleFunc("/api/actions/pause", s.auth(method(http.MethodPost, s.pause)))
	mux.HandleFunc("/api/actions/", s.auth(method(http.MethodGet, s.action)))
	mux.HandleFunc("/api/node", s.auth(s.node))
	mux.HandleFunc("/api/node/import", s.auth(method(http.MethodPost, s.importNode)))
	mux.HandleFunc("/api/backups", s.auth(s.backups))
	mux.HandleFunc("/api/backups/", s.auth(method(http.MethodPost, s.restoreBackup)))
	mux.HandleFunc("/", s.auth(s.frontend))
	return securityHeaders(sameOriginWrites(mux))
}

func (s *Server) importNode(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var document model.VPSConfig
	if err := decoder.Decode(&document); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid VPS configuration: " + err.Error()})
		return
	}
	configuration, err := s.controller.ImportVPSConfig(r.Context(), document)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, configuration)
}

func (s *Server) backups(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		backups, err := s.controller.ListBackups()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, backups)
	case http.MethodPost:
		backup, err := s.controller.CreateBackup("manual")
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, backup)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) restoreBackup(w http.ResponseWriter, r *http.Request) {
	suffix := strings.TrimPrefix(r.URL.Path, "/api/backups/")
	id, ok := strings.CutSuffix(suffix, "/restore")
	if !ok || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	configuration, err := s.controller.RestoreBackup(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, configuration)
}

func (s *Server) node(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		configuration, err := s.controller.NodeConfiguration()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, configuration)
	case http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var update model.NodeUpdate
		if err := decoder.Decode(&update); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid node configuration: " + err.Error()})
			return
		}
		configuration, err := s.controller.UpdateNode(r.Context(), update)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, configuration)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func method(allowed string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != allowed {
			w.Header().Set("Allow", allowed)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		next(w, r)
	}
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) snapshot(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.controller.Snapshot())
}

func (s *Server) runTests(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.controller.CheckNow(r.Context()))
}

func (s *Server) restart(w http.ResponseWriter, r *http.Request) {
	s.queueLifecycleAction(w, "restart")
}

func (s *Server) start(w http.ResponseWriter, _ *http.Request) {
	s.queueLifecycleAction(w, "start")
}

func (s *Server) pause(w http.ResponseWriter, _ *http.Request) {
	s.queueLifecycleAction(w, "pause")
}

func (s *Server) queueLifecycleAction(w http.ResponseWriter, kind string) {
	action, err := s.controller.QueueLifecycleAction(kind)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, action)
}

func (s *Server) action(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/actions/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	action, err := s.controller.LifecycleAction(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, action)
}

func (s *Server) frontend(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}
	if r.URL.Path != "/" {
		if _, err := fs.Stat(mustSub(embedded, "static"), strings.TrimPrefix(r.URL.Path, "/")); err != nil {
			r.URL.Path = "/"
		}
	}
	s.static.ServeHTTP(w, r)
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.disableAuth {
			next(w, r)
			return
		}
		if s.password == "" {
			http.Error(w, "admin password is not configured", http.StatusServiceUnavailable)
			return
		}
		user, pass, ok := r.BasicAuth()
		userOK := subtle.ConstantTimeCompare([]byte(user), []byte(s.user)) == 1
		passOK := subtle.ConstantTimeCompare([]byte(pass), []byte(s.password)) == 1
		if !ok || !userOK || !passOK {
			w.Header().Set("WWW-Authenticate", `Basic realm="LightNode Controller"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func sameOriginWrites(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			origin := strings.TrimSuffix(r.Header.Get("Origin"), "/")
			if origin != "" && origin != "http://"+r.Host && origin != "https://"+r.Host {
				http.Error(w, "cross-origin write rejected", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func mustSub(source fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(source, dir)
	if err != nil {
		panic(err)
	}
	return sub
}
