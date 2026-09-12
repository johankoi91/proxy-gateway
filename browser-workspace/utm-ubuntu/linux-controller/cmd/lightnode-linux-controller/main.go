package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
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
		if err := serveContainer(ctx, runtime); err != nil {
			fatalf("container-run: %v", err)
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
		_ = flags.Parse(os.Args[2:])
		if *vpsConfig == "" {
			fatalf("--vps-config is required")
		}
		if err := manager.Provision(*vpsConfig, *output); err != nil {
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
	server := &http.Server{Addr: snapshot.Configuration.ManagementAddress + ":9180", Handler: httpapi.New(runtime), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
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

func usage() {
	fmt.Fprintln(os.Stderr, "usage: lightnode-linux-controller <container-run|provision|validate|firewall-apply|dns-apply|dns-wait-apply|dns-ensure|host-refresh|doctor>")
	os.Exit(2)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
