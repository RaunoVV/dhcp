// Package handler holds the interface that backends implement, handlers take in, and the top level dhcp package passes to handlers.
package handler

import (
	"context"
	"github.com/tinkerbell/tink/api/v1alpha1"
	"net"

	"github.com/raunovv/dhcp/data"
)

// BackendReader is the interface for getting data from a backend.
//
// Backends implement this interface to provide DHCP and Netboot data to the handlers.
type BackendReader interface {
	// Read data (from a backend) based on a mac address
	// and return DHCP headers and options, including netboot info.
	GetByMac(context.Context, net.HardwareAddr) (*data.DHCP, *data.Netboot, error)
	GetByIP(context.Context, net.IP) (*data.DHCP, *data.Netboot, error)
	RegisterHw(context.Context, v1alpha1.Hardware) error
}
