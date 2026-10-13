package main

import (
	"crypto/tls"
	"encoding/json"
	"github.com/gorilla/websocket"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"
)

var upstream = "https://127.0.0.1:9186"
var token = os.Getenv("CLAUDE_BROWSER_API_TOKEN")
var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

func proxyHandler() http.Handler {
	target, _ := url.Parse(upstream)
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, ResponseHeaderTimeout: 180 * time.Second}
	proxy.Director = func(r *http.Request) {
		r.URL.Scheme = target.Scheme
		r.URL.Host = target.Host
		r.URL.Path = strings.TrimPrefix(r.URL.Path, "/api")
		if r.URL.Path != "/healthz" && !strings.HasPrefix(r.URL.Path, "/v1") {
			r.URL.Path = "/v1" + r.URL.Path
		}
		r.Host = target.Host
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("X-Gateway-Request-ID", time.Now().Format("20060102150405.000000000"))
		proxy.ServeHTTP(w, r)
	})
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	client, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer client.Close()
	backendURL := "wss://127.0.0.1:9186/v1/ws"
	backend, _, err := websocket.DefaultDialer.Dial(backendURL, nil)
	if err != nil {
		_ = client.WriteJSON(map[string]string{"type": "error", "message": "browser api websocket unavailable"})
		return
	}
	defer backend.Close()
	done := make(chan struct{}, 2)
	pipe := func(dst, src *websocket.Conn) {
		defer func() { done <- struct{}{} }()
		for {
			typ, data, err := src.ReadMessage()
			if err != nil {
				return
			}
			if typ == websocket.TextMessage {
				var event map[string]any
				if json.Unmarshal(data, &event) == nil && event["type"] == "auth" {
					event["token"] = token
					data, _ = json.Marshal(event)
				}
			}
			if err := dst.WriteMessage(typ, data); err != nil {
				return
			}
		}
	}
	go pipe(client, backend)
	go pipe(backend, client)
	<-done
}

func main() {
	mux := http.NewServeMux()
	mux.Handle("/api/ws", http.HandlerFunc(wsHandler))
	mux.Handle("/api/", proxyHandler())
	mux.Handle("/", http.FileServer(http.Dir("/app/dist")))
	srv := &http.Server{Addr: ":5190", Handler: mux, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 190 * time.Second, WriteTimeout: 190 * time.Second}
	log.Println("claude web gateway listening on :5190")
	log.Fatal(srv.ListenAndServe())
}

var _ io.Reader
