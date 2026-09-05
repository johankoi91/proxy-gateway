package httpapi

import (
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/skip2/go-qrcode"

	"lightnode-vpn-gateway/internal/gateway"
)

//go:embed static/*
var staticFiles embed.FS

type Server struct {
	manager *gateway.Manager
	user    string
	pass    string
	version string
}

func New(manager *gateway.Manager, user, pass, version string) http.Handler {
	s := &Server{manager: manager, user: user, pass: pass, version: version}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.Handle("GET /api/status", s.auth(http.HandlerFunc(s.status)))
	mux.Handle("GET /api/clients", s.auth(http.HandlerFunc(s.clients)))
	mux.Handle("POST /api/clients", s.auth(http.HandlerFunc(s.createClient)))
	mux.Handle("DELETE /api/clients/{id}", s.auth(http.HandlerFunc(s.deleteClient)))
	mux.Handle("GET /api/clients/{id}/wireguard.conf", s.auth(http.HandlerFunc(s.wireGuardConfig)))
	mux.Handle("GET /api/clients/{id}/wireguard.png", s.auth(http.HandlerFunc(s.wireGuardQR)))
	mux.Handle("GET /api/clients/{id}/vless.txt", s.auth(http.HandlerFunc(s.vlessURI)))
	mux.Handle("GET /api/clients/{id}/vless.png", s.auth(http.HandlerFunc(s.vlessQR)))
	mux.Handle("GET /api/clients/{id}/clash.yaml", s.auth(http.HandlerFunc(s.clashConfig)))
	mux.Handle("/", s.auth(s.staticHandler()))
	return securityHeaders(mux)
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		userOK := subtle.ConstantTimeCompare([]byte(user), []byte(s.user)) == 1
		passOK := subtle.ConstantTimeCompare([]byte(pass), []byte(s.pass)) == 1
		if !ok || !userOK || !passOK {
			w.Header().Set("WWW-Authenticate", `Basic realm="VPN Gateway", charset="UTF-8"`)
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": s.version})
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, s.manager.Status(r.Context()))
}

func (s *Server) clients(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, s.manager.ListClients())
}

func (s *Server) createClient(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name string `json:"name"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	client, err := s.manager.AddClient(r.Context(), input.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, client)
}

func (s *Server) deleteClient(w http.ResponseWriter, r *http.Request) {
	err := s.manager.DeleteClient(r.Context(), r.PathValue("id"))
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusNotFound, "client not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) wireGuardConfig(w http.ResponseWriter, r *http.Request) {
	s.serveClientText(w, r, "text/plain; charset=utf-8", ".conf", func(client gateway.Client) string {
		return s.manager.WireGuardClientConfig(client)
	})
}

func (s *Server) wireGuardQR(w http.ResponseWriter, r *http.Request) {
	s.serveClientQR(w, r, func(client gateway.Client) string {
		return s.manager.WireGuardClientConfig(client)
	})
}

func (s *Server) vlessURI(w http.ResponseWriter, r *http.Request) {
	s.serveClientText(w, r, "text/plain; charset=utf-8", "-vless.txt", s.manager.VLESSURI)
}

func (s *Server) vlessQR(w http.ResponseWriter, r *http.Request) {
	s.serveClientQR(w, r, s.manager.VLESSURI)
}

func (s *Server) clashConfig(w http.ResponseWriter, r *http.Request) {
	s.serveClientText(w, r, "application/yaml; charset=utf-8", "-clash.yaml", s.manager.ClashConfig)
}

func (s *Server) serveClientText(w http.ResponseWriter, r *http.Request, contentType, suffix string, render func(gateway.Client) string) {
	client, ok := s.manager.Client(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "client not found")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s%s"`, safeFilename(client.Name), suffix))
	_, _ = w.Write([]byte(render(client)))
}

func (s *Server) serveClientQR(w http.ResponseWriter, r *http.Request, render func(gateway.Client) string) {
	client, ok := s.manager.Client(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "client not found")
		return
	}
	png, err := qrcode.Encode(render(client), qrcode.Medium, 512)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not generate QR code")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(png)
}

func (s *Server) staticHandler() http.Handler {
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" {
			if _, err := fs.Stat(sub, path); err == nil {
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		index, err := fs.ReadFile(sub, "index.html")
		if err != nil {
			http.Error(w, "frontend unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(index)
	})
}

func safeFilename(name string) string {
	name = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, name)
	name = strings.Trim(name, "-")
	if name == "" {
		return "vpn-client"
	}
	return name
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message, "timestamp": time.Now().UTC()})
}
