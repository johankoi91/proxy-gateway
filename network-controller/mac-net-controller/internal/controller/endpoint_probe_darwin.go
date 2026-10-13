//go:build darwin

package controller

import (
	"context"
	"fmt"
	"net"
	"syscall"
	"time"
)

func dialPhysicalEndpoint(ctx context.Context, interfaceName, address string, timeout time.Duration) (net.Conn, error) {
	physical, err := net.InterfaceByName(interfaceName)
	if err != nil {
		return nil, fmt.Errorf("resolve interface %s: %w", interfaceName, err)
	}
	dialer := net.Dialer{
		Timeout: timeout,
		Control: func(_, _ string, raw syscall.RawConn) error {
			var socketErr error
			if err := raw.Control(func(fd uintptr) {
				socketErr = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, syscall.IP_BOUND_IF, physical.Index)
			}); err != nil {
				return err
			}
			return socketErr
		},
	}
	return dialer.DialContext(ctx, "tcp4", address)
}
