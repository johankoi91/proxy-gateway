//go:build !darwin

package controller

import (
	"context"
	"errors"
	"net"
	"time"
)

func dialPhysicalEndpoint(context.Context, string, string, time.Duration) (net.Conn, error) {
	return nil, errors.New("physical endpoint probe requires Darwin IP_BOUND_IF")
}
