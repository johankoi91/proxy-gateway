package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"lightnode-vpn-gateway/internal/gateway"
	"lightnode-vpn-gateway/internal/httpapi"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		fatalf("usage: vpn-gateway <bootstrap|serve>")
	}
	cfg, store, manager := buildManager()
	switch os.Args[1] {
	case "bootstrap":
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if err := manager.Bootstrap(ctx); err != nil {
			fatalf("bootstrap: %v", err)
		}
		log.Printf("gateway state ready at %s", store.Path())
	case "serve":
		if err := manager.Load(); err != nil {
			fatalf("load state: %v", err)
		}
		serve(cfg, manager)
	default:
		fatalf("unknown command %q", os.Args[1])
	}
}

func buildManager() (gateway.Config, *gateway.Store, *gateway.Manager) {
	cfg := gateway.DefaultConfig()
	cfg.RuntimeDir = env("RUNTIME_DIR", cfg.RuntimeDir)
	cfg.PublicIP = strings.TrimSpace(os.Getenv("PUBLIC_IP"))
	cfg.EgressInterface = strings.TrimSpace(os.Getenv("EGRESS_INTERFACE"))
	cfg.WireGuardPort = envInt("WG_PORT", cfg.WireGuardPort)
	cfg.RealityPort = envInt("VLESS_PORT", cfg.RealityPort)
	cfg.RealityListen = envInt("VLESS_LISTEN_PORT", cfg.RealityPort)
	cfg.RealityTarget = env("REALITY_TARGET", cfg.RealityTarget)
	cfg.RealityName = env("REALITY_SERVER_NAME", cfg.RealityName)
	cfg.DryRun = strings.EqualFold(os.Getenv("DRY_RUN"), "true")
	if value := strings.TrimSpace(os.Getenv("DEFAULT_CLIENTS")); value != "" {
		cfg.DefaultClients = nil
		for _, name := range strings.Split(value, ",") {
			if name = strings.TrimSpace(name); name != "" {
				cfg.DefaultClients = append(cfg.DefaultClients, name)
			}
		}
	}
	statePath := env("STATE_PATH", "/data/state.json")
	store := gateway.NewStore(statePath)
	return cfg, store, gateway.NewManager(store, gateway.ExecRunner{}, cfg)
}

func serve(cfg gateway.Config, manager *gateway.Manager) {
	user := env("ADMIN_USER", "admin")
	pass := strings.TrimSpace(os.Getenv("ADMIN_PASSWORD"))
	if pass == "" || pass == "change-me" {
		fatalf("ADMIN_PASSWORD must be set to a non-default value")
	}
	listen := env("LISTEN_ADDR", "127.0.0.1:9080")
	tlsCert := strings.TrimSpace(os.Getenv("TLS_CERT_FILE"))
	tlsKey := strings.TrimSpace(os.Getenv("TLS_KEY_FILE"))
	if (tlsCert == "") != (tlsKey == "") {
		fatalf("TLS_CERT_FILE and TLS_KEY_FILE must be set together")
	}
	server := &http.Server{
		Addr:              listen,
		Handler:           httpapi.New(manager, user, pass, version),
		TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12},
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)
	protocol := "http"
	if tlsCert != "" {
		protocol = "https"
	}
	log.Printf("VPN Gateway %s listening on %s://%s (dry-run=%t)", version, protocol, listen, cfg.DryRun)
	go func() {
		var err error
		if tlsCert != "" {
			err = server.ListenAndServeTLS(tlsCert, tlsKey)
		} else {
			err = server.ListenAndServe()
		}
		if !errors.Is(err, http.ErrServerClosed) {
			fatalf("serve: %v", err)
		}
	}()
	<-done
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
}

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 1 || n > 65535 {
		fatalf("%s must be a valid port", name)
	}
	return n
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
