package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"lightnode-linux-controller/internal/httpapi"
	"lightnode-linux-controller/internal/system"
)

const defaultConfig = "/etc/lightnode-linux-controller/controller.json"

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	manager := system.New(system.ExecRunner{})
	if os.Args[1] == "container-run" {
		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()
		runtime, err := system.NewContainerRuntime(manager, defaultConfig)
		if err != nil {
			fatalf("container-run: %v", err)
		}
		if err := runtime.StartInitial(ctx); err != nil {
			fatalf("container-run: %v", err)
		}
		serveErr := serveContainer(ctx, runtime)
		shutdownErr := runtime.Shutdown()
		if serveErr != nil {
			fatalf("container-run: %v", serveErr)
		}
		if shutdownErr != nil {
			fatalf("container shutdown: %v", shutdownErr)
		}
		return
	}
	if os.Args[1] == "container-health" {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if err := system.ContainerHealth(ctx, defaultConfig); err != nil {
			fatalf("container-health: %v", err)
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	switch os.Args[1] {
	case "provision":
		flags := flag.NewFlagSet("provision", flag.ExitOnError)
		vpsConfig := flags.String("vps-config", "", "lightnode.vps-config JSON file")
		output := flags.String("output", "/etc/lightnode-linux-controller", "output directory")
		publicBrowser := flags.Bool("public-browser", false, "allow public Selkies ingress and bind dashboard to loopback")
		_ = flags.Parse(os.Args[2:])
		if *vpsConfig == "" {
			fatalf("--vps-config is required")
		}
		if err := manager.Provision(*vpsConfig, *output, *publicBrowser); err != nil {
			fatalf("provision: %v", err)
		}
		fmt.Printf("configuration written to %s; network was not changed\n", *output)
	case "validate", "firewall-apply", "dns-apply", "dns-wait-apply", "dns-ensure", "host-refresh", "doctor":
		flags := flag.NewFlagSet(os.Args[1], flag.ExitOnError)
		configPath := flags.String("config", defaultConfig, "controller configuration")
		_ = flags.Parse(os.Args[2:])
		var err error
		switch os.Args[1] {
		case "validate":
			err = manager.Validate(ctx, *configPath)
		case "firewall-apply":
			err = manager.ApplyFirewall(ctx, *configPath)
		case "dns-apply":
			err = manager.ApplyDNSConfig(ctx, *configPath)
		case "dns-wait-apply":
			err = manager.WaitAndApplyDNS(ctx, *configPath)
		case "dns-ensure":
			err = manager.EnsureDNS(ctx, *configPath)
		case "host-refresh":
			err = manager.RefreshHostStatus(ctx, *configPath)
		case "doctor":
			report := manager.Doctor(ctx, *configPath)
			encoded, encodeErr := system.EncodeReport(report)
			if encodeErr != nil {
				fatalf("doctor: %v", encodeErr)
			}
			fmt.Println(string(encoded))
			if !report.Protected || !report.Connected {
				os.Exit(1)
			}
		}
		if err != nil {
			fatalf("%s: %v", os.Args[1], err)
		}
	default:
		usage()
	}
}

func serveContainer(ctx context.Context, runtime *system.ContainerRuntime) error {
	snapshot := runtime.Snapshot()
	address := snapshot.Configuration.DashboardAddress
	username := os.Getenv("LIGHTNODE_CONTROLLER_ADMIN_USER")
	password := os.Getenv("LIGHTNODE_CONTROLLER_ADMIN_PASSWORD")
	certificate := os.Getenv("LIGHTNODE_CONTROLLER_TLS_CERT")
	privateKey := os.Getenv("LIGHTNODE_CONTROLLER_TLS_KEY")
	public := ipIsPublicListener(address)
	if public && (username == "" || password == "") {
		return errors.New("public dashboard requires basic authentication credentials")
	}
	if public && (certificate == "" || privateKey == "") {
		return errors.New("public dashboard requires a TLS certificate and private key")
	}
	handler := httpapi.New(runtime)
	if username != "" || password != "" {
		if username == "" || password == "" {
			return errors.New("both dashboard username and password are required")
		}
		handler = httpapi.WithBasicAuth(handler, username, password)
	}
	server := &http.Server{Addr: address + ":9180", Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	done := make(chan error, 1)
	go func() {
		if certificate != "" || privateKey != "" {
			done <- server.ListenAndServeTLS(certificate, privateKey)
			return
		}
		done <- server.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func ipIsPublicListener(address string) bool {
	ip := net.ParseIP(address)
	return ip == nil || !ip.IsLoopback()
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: lightnode-linux-controller <container-run|provision|validate|firewall-apply|dns-apply|dns-wait-apply|dns-ensure|host-refresh|doctor>")
	os.Exit(2)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
