package sysmanage

import (
	"context"
	"fmt"
	"strconv"
)

func IsCommand(value string) bool {
	switch value {
	case "tunnel", "firewall", "dns", "conflicts", "notify", "install", "uninstall", "regression":
		return true
	default:
		return false
	}
}

func (m *Manager) RunCLI(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("system command is required")
	}
	switch args[0] {
	case "tunnel":
		if len(args) != 2 {
			return fmt.Errorf("usage: lightnode-controller tunnel start|pause|restart")
		}
		switch args[1] {
		case "start":
			return m.StartTunnel(ctx)
		case "pause":
			return m.PauseTunnel(ctx)
		case "restart":
			return m.RestartTunnel(ctx)
		}
	case "firewall":
		if len(args) < 2 {
			break
		}
		switch args[1] {
		case "bootstrap":
			return m.BootstrapFirewall(ctx)
		case "refresh":
			return m.RefreshFirewall(ctx, hasFlag(args[2:], "--force"))
		case "enable":
			seconds := 180
			for index := 2; index+1 < len(args); index++ {
				if args[index] == "--rollback-seconds" {
					value, err := strconv.Atoi(args[index+1])
					if err != nil || value < 1 || value > 3600 {
						return fmt.Errorf("invalid rollback seconds %q", args[index+1])
					}
					seconds = value
				}
			}
			return m.EnableFirewall(ctx, seconds)
		case "confirm":
			return m.ConfirmFirewall(ctx)
		case "disable":
			return m.DisableFirewall(ctx, hasFlag(args[2:], "--yes"))
		case "rollback":
			if len(args) < 3 || len(args) > 4 {
				return fmt.Errorf("usage: lightnode-controller firewall rollback TOKEN [SECONDS]")
			}
			delay := 180
			if len(args) == 4 {
				value, err := strconv.Atoi(args[3])
				if err != nil || value < 0 || value > 3600 {
					return fmt.Errorf("invalid rollback delay %q", args[3])
				}
				delay = value
			}
			return m.RollbackFirewall(ctx, args[2], delay)
		}
	case "dns":
		if len(args) != 2 {
			break
		}
		switch args[1] {
		case "refresh":
			return m.RefreshDNS(ctx, false)
		case "restore":
			return m.RefreshDNS(ctx, true)
		}
	case "conflicts":
		if len(args) == 2 && args[1] == "remediate" {
			return m.RemediateConflicts(ctx)
		}
	case "notify":
		severity, component, message := "warning", "controller", "LightNode Controller state changed"
		if len(args) > 1 {
			severity = args[1]
		}
		if len(args) > 2 {
			component = args[2]
		}
		if len(args) > 3 {
			message = args[3]
		}
		return m.Notify(ctx, severity, component, message)
	case "install":
		if len(args) == 2 && args[1] == "postinstall" {
			return m.PostInstall(ctx)
		}
	case "uninstall":
		if len(args) != 2 || args[1] != "--yes" {
			return fmt.Errorf("usage: lightnode-controller uninstall --yes")
		}
		return m.Uninstall(ctx, true)
	case "regression":
		if len(args) >= 2 && args[1] == "recover" {
			delay := 0
			for index := 2; index+1 < len(args); index++ {
				if args[index] == "--delay-seconds" {
					value, err := strconv.Atoi(args[index+1])
					if err != nil || value < 0 || value > 300 {
						return fmt.Errorf("invalid recovery delay %q", args[index+1])
					}
					delay = value
				}
			}
			return m.RecoverRegression(ctx, delay)
		}
		if len(args) >= 2 && args[1] == "run" {
			return m.RunRegression(ctx, hasFlag(args[2:], "--yes"), hasFlag(args[2:], "--include-network-drop"))
		}
	}
	return fmt.Errorf("unsupported command: %v", args)
}

func hasFlag(args []string, expected string) bool {
	for _, value := range args {
		if value == expected {
			return true
		}
	}
	return false
}
