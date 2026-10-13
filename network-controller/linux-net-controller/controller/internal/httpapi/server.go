package httpapi

import (
	"crypto/subtle"
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"

	"lightnode-linux-controller/internal/system"
)

//go:embed static/*
var embedded embed.FS

func New(runtime *system.ContainerRuntime) http.Handler {
	staticFS, _ := fs.Sub(embedded, "static")
	files := http.FileServer(http.FS(staticFS))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := runtime.ContainerHealth(r.Context()); err != nil {
			writeJSON(w, 503, map[string]string{"status": "unhealthy", "error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/snapshot", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, runtime.Snapshot()) })
	mux.HandleFunc("POST /api/check", func(w http.ResponseWriter, _ *http.Request) {
		runtime.RequestHostRefresh()
		writeJSON(w, 202, map[string]string{"status": "queued"})
	})
	mux.HandleFunc("POST /api/actions/{kind}", func(w http.ResponseWriter, r *http.Request) {
		action, err := runtime.QueueAction(r.PathValue("kind"))
		if err != nil {
			writeJSON(w, 409, map[string]any{"error": err.Error(), "action": action})
			return
		}
		writeJSON(w, 202, action)
	})
	mux.HandleFunc("GET /api/actions/{id}", func(w http.ResponseWriter, r *http.Request) {
		action, ok := runtime.Action(r.PathValue("id"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, 200, action)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" {
			if _, err := fs.Stat(staticFS, path); err != nil {
				r.URL.Path = "/"
			}
		}
		files.ServeHTTP(w, r)
	})
	return security(sameOrigin(mux))
}

func WithBasicAuth(next http.Handler, username, password string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providedUser, providedPassword, ok := r.BasicAuth()
		userOK := subtle.ConstantTimeCompare([]byte(providedUser), []byte(username)) == 1
		passwordOK := subtle.ConstantTimeCompare([]byte(providedPassword), []byte(password)) == 1
		if !ok || !userOK || !passwordOK {
			w.Header().Set("WWW-Authenticate", `Basic realm="LightNode Controller", charset="UTF-8"`)
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			origin := strings.TrimSuffix(r.Header.Get("Origin"), "/")
			if origin != "" && origin != "http://"+r.Host && origin != "https://"+r.Host {
				http.Error(w, "cross-origin write rejected", 403)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; connect-src 'self'; img-src 'self' data:; base-uri 'none'; frame-ancestors 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
