package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"lightnode-mac-controller/internal/config"
	"lightnode-mac-controller/internal/controller"
	"lightnode-mac-controller/internal/httpapi"
	"lightnode-mac-controller/internal/sysmanage"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 && sysmanage.IsCommand(os.Args[1]) {
		manager := sysmanage.New(controller.ExecRunner{})
		if err := manager.RunCLI(context.Background(), os.Args[1:]); err != nil {
			fatalf("%v", err)
		}
		return
	}
	configPath := flag.String("config", "/Library/Application Support/LightNodeController/config.json", "configuration file")
	once := flag.Bool("once", false, "run checks once and print JSON")
	flag.Parse()
	cfg, err := config.Load(*configPath)
	if err != nil {
		fatalf("load config: %v", err)
	}
	appController := controller.NewController(cfg, controller.ExecRunner{}, version)
	if *once {
		snapshot := appController.CheckNow(context.Background())
		_ = json.NewEncoder(os.Stdout).Encode(snapshot)
		return
	}
	if !cfg.DisableAuthentication && (cfg.AdminPassword == "" || cfg.AdminPassword == "CHANGE_ME") {
		fatalf("adminPassword or LIGHTNODE_CONTROLLER_ADMIN_PASSWORD is required")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	appController.Start(ctx)
	server := &http.Server{
		Addr:              cfg.ListenAddress,
		Handler:           httpapi.New(appController, cfg.AdminUser, cfg.AdminPassword, cfg.DisableAuthentication),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      180 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		log.Printf("LightNode Controller %s listening on http://%s", version, cfg.ListenAddress)
		if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			fatalf("serve: %v", err)
		}
	}()
	<-ctx.Done()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = server.Shutdown(shutdownCtx)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
