package httpapi

import (
	"crypto/subtle"
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"

	"lightnode-mac-guard/internal/guard"
)

//go:embed static/* static/assets/*
var embedded embed.FS

type Server struct {
	controller  *guard.Controller
	user        string
	password    string
	disableAuth bool
	static      http.Handler
}

func New(controller *guard.Controller, user, password string, disableAuth bool) http.Handler {
	staticFS, _ := fs.Sub(embedded, "static")
	s := &Server{controller: controller, user: user, password: password, disableAuth: disableAuth, static: http.FileServer(http.FS(staticFS))}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", method(http.MethodGet, s.health))
	mux.HandleFunc("/api/snapshot", s.auth(method(http.MethodGet, s.snapshot)))
	mux.HandleFunc("/api/tests/run", s.auth(method(http.MethodPost, s.runTests)))
	mux.HandleFunc("/api/actions/restart", s.auth(method(http.MethodPost, s.restart)))
	mux.HandleFunc("/api/actions/start", s.auth(method(http.MethodPost, s.start)))
	mux.HandleFunc("/api/actions/pause", s.auth(method(http.MethodPost, s.pause)))
	mux.HandleFunc("/api/v2/policy", s.auth(method(http.MethodGet, s.policyV2)))
	mux.HandleFunc("/", s.auth(s.frontend))
	return securityHeaders(sameOriginWrites(mux))
}

func (s *Server) policyV2(w http.ResponseWriter, _ *http.Request) {
	snapshot, err := s.controller.PolicySnapshot()
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
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
	if err := s.controller.RestartTunnel(r.Context()); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s.controller.Snapshot())
}

func (s *Server) start(w http.ResponseWriter, r *http.Request) {
	if err := s.controller.StartTunnel(r.Context()); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s.controller.Snapshot())
}

func (s *Server) pause(w http.ResponseWriter, r *http.Request) {
	if err := s.controller.PauseTunnel(r.Context()); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s.controller.Snapshot())
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
			w.Header().Set("WWW-Authenticate", `Basic realm="LightNode Guard"`)
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
