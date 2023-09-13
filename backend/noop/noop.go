// Package noop is a backend handler that does nothing.
package noop

import (
	"context"
	"errors"
	"github.com/tinkerbell/tink/api/v1alpha1"
	"net"

	"github.com/raunovv/dhcp/data"
)

// Handler is a noop backend.
type Handler struct{}

func (h Handler) RegisterHw(ctx context.Context, hwObject v1alpha1.Hardware) error {
	return errors.New("no backend specified, please specify a backend")
}

// GetByMac returns an error.
func (h Handler) GetByMac(_ context.Context, _ net.HardwareAddr) (*data.DHCP, *data.Netboot, error) {
	return nil, nil, errors.New("no backend specified, please specify a backend")
}

// GetByIP returns an error.
func (h Handler) GetByIP(_ context.Context, _ net.IP) (*data.DHCP, *data.Netboot, error) {
	return nil, nil, errors.New("no backend specified, please specify a backend")
}
