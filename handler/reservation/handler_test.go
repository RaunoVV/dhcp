package reservation

import (
	"context"
	"errors"
	"fmt"
	"github.com/tinkerbell/tink/api/v1alpha1"
	"log"
	"net"
	"net/netip"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/go-logr/stdr"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/iana"
	"github.com/insomniacslk/dhcp/rfc1035label"
	"github.com/raunovv/dhcp/data"
	"github.com/raunovv/dhcp/otel"
	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/net/nettest"
)

var errBadBackend = fmt.Errorf("bad backend")
var errMacNotFound = func(mac net.HardwareAddr, err string) error {
	return fmt.Errorf("failed listing hardware for (%v): %s", mac, err)
}

type mockBackend struct {
	err          error
	allowNetboot bool
	ipxeScript   *url.URL
}

func (m *mockBackend) RegisterHw(ctx context.Context, hardware v1alpha1.Hardware) error {
	//TODO implement me
	return nil
}

func (m *mockBackend) GetByMac(context.Context, net.HardwareAddr) (*data.DHCP, *data.Netboot, error) {
	if m.err != nil {
		return nil, nil, m.err
	}
	d := &data.DHCP{
		MACAddress:     []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
		IPAddress:      netip.MustParseAddr("192.168.1.100"),
		SubnetMask:     []byte{255, 255, 255, 0},
		DefaultGateway: netip.MustParseAddr("192.168.1.1"),
		NameServers: []net.IP{
			{1, 1, 1, 1},
		},
		Hostname:         "test-host",
		DomainName:       "mydomain.com",
		BroadcastAddress: netip.MustParseAddr("192.168.1.255"),
		NTPServers: []net.IP{
			{132, 163, 96, 2},
		},
		LeaseTime: 60,
		DomainSearch: []string{
			"mydomain.com",
		},
	}
	n := &data.Netboot{
		AllowNetboot:  m.allowNetboot,
		IPXEScriptURL: m.ipxeScript,
	}
	return d, n, m.err
}

func (m *mockBackend) GetByIP(context.Context, net.IP) (*data.DHCP, *data.Netboot, error) {
	return nil, nil, errors.New("not implemented")
}

func TestHandle(t *testing.T) {
	tests := map[string]struct {
		server  Handler
		req     *dhcpv4.DHCPv4
		want    *dhcpv4.DHCPv4
		wantErr error
		nilPeer bool
	}{
		"success discover message type": {
			server: Handler{
				Backend: &mockBackend{},
				IPAddr:  netip.MustParseAddr("127.0.0.1"),
			},
			req: &dhcpv4.DHCPv4{
				OpCode:       dhcpv4.OpcodeBootRequest,
				ClientHWAddr: []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
				Options: dhcpv4.OptionsFromList(
					dhcpv4.OptMessageType(dhcpv4.MessageTypeDiscover),
				),
			},
			want: &dhcpv4.DHCPv4{
				OpCode:        dhcpv4.OpcodeBootReply,
				ClientHWAddr:  []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
				ClientIPAddr:  []byte{0, 0, 0, 0},
				YourIPAddr:    []byte{192, 168, 1, 100},
				ServerIPAddr:  []byte{127, 0, 0, 1},
				GatewayIPAddr: []byte{0, 0, 0, 0},
				Options: dhcpv4.OptionsFromList(
					dhcpv4.OptMessageType(dhcpv4.MessageTypeOffer),
					dhcpv4.OptServerIdentifier(net.IP{127, 0, 0, 1}),
					dhcpv4.OptIPAddressLeaseTime(time.Minute),
					dhcpv4.OptSubnetMask(net.IPMask(net.IP{255, 255, 255, 0}.To4())),
					dhcpv4.OptRouter([]net.IP{{192, 168, 1, 1}}...),
					dhcpv4.OptDNS([]net.IP{{1, 1, 1, 1}}...),
					dhcpv4.OptDomainName("mydomain.com"),
					dhcpv4.OptHostName("test-host"),
					dhcpv4.OptBroadcastAddress(net.IP{192, 168, 1, 255}),
					dhcpv4.OptNTPServers([]net.IP{{132, 163, 96, 2}}...),
					dhcpv4.OptDomainSearch(&rfc1035label.Labels{Labels: []string{"mydomain.com"}}),
				),
			},
		},
		"failure discover message type": {
			server: Handler{
				Backend: &mockBackend{err: errBadBackend},
				IPAddr:  netip.MustParseAddr("127.0.0.1"),
			},
			req: &dhcpv4.DHCPv4{
				OpCode:       dhcpv4.OpcodeBootRequest,
				ClientHWAddr: []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
				Options: dhcpv4.OptionsFromList(
					dhcpv4.OptMessageType(dhcpv4.MessageTypeDiscover),
				),
			},
			wantErr: errBadBackend,
		},
		"success request message type": {
			server: Handler{
				Backend: &mockBackend{},
				IPAddr:  netip.MustParseAddr("127.0.0.1"),
			},
			req: &dhcpv4.DHCPv4{
				OpCode:        dhcpv4.OpcodeBootRequest,
				ClientHWAddr:  []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
				ClientIPAddr:  []byte{0, 0, 0, 0},
				YourIPAddr:    []byte{192, 168, 1, 100},
				ServerIPAddr:  []byte{127, 0, 0, 1},
				GatewayIPAddr: []byte{0, 0, 0, 0},
				Options: dhcpv4.OptionsFromList(
					dhcpv4.OptMessageType(dhcpv4.MessageTypeRequest),
					dhcpv4.OptServerIdentifier(net.IP{127, 0, 0, 1}),
					dhcpv4.OptIPAddressLeaseTime(time.Minute),
					dhcpv4.OptSubnetMask(net.IPMask(net.IP{255, 255, 255, 0}.To4())),
					dhcpv4.OptRouter([]net.IP{{192, 168, 1, 1}}...),
					dhcpv4.OptDNS([]net.IP{{1, 1, 1, 1}}...),
					dhcpv4.OptDomainName("mydomain.com"),
					dhcpv4.OptHostName("test-host"),
					dhcpv4.OptBroadcastAddress(net.IP{192, 168, 1, 255}),
					dhcpv4.OptNTPServers([]net.IP{{132, 163, 96, 2}}...),
					dhcpv4.OptDomainSearch(&rfc1035label.Labels{Labels: []string{"mydomain.com"}}),
				),
			},
			want: &dhcpv4.DHCPv4{
				OpCode:        dhcpv4.OpcodeBootReply,
				ClientHWAddr:  []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
				ClientIPAddr:  []byte{0, 0, 0, 0},
				YourIPAddr:    []byte{192, 168, 1, 100},
				ServerIPAddr:  []byte{127, 0, 0, 1},
				GatewayIPAddr: []byte{0, 0, 0, 0},
				Options: dhcpv4.OptionsFromList(
					dhcpv4.OptMessageType(dhcpv4.MessageTypeAck),
					dhcpv4.OptServerIdentifier(net.IP{127, 0, 0, 1}),
					dhcpv4.OptIPAddressLeaseTime(time.Minute),
					dhcpv4.OptSubnetMask(net.IPMask(net.IP{255, 255, 255, 0}.To4())),
					dhcpv4.OptRouter([]net.IP{{192, 168, 1, 1}}...),
					dhcpv4.OptDNS([]net.IP{{1, 1, 1, 1}}...),
					dhcpv4.OptDomainName("mydomain.com"),
					dhcpv4.OptHostName("test-host"),
					dhcpv4.OptBroadcastAddress(net.IP{192, 168, 1, 255}),
					dhcpv4.OptNTPServers([]net.IP{{132, 163, 96, 2}}...),
					dhcpv4.OptDomainSearch(&rfc1035label.Labels{Labels: []string{"mydomain.com"}}),
				),
			},
		},
		"failure request message type": {
			server: Handler{
				Backend: &mockBackend{err: errBadBackend},
				IPAddr:  netip.MustParseAddr("127.0.0.1"),
			},
			req: &dhcpv4.DHCPv4{
				OpCode:       dhcpv4.OpcodeBootRequest,
				ClientHWAddr: []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
				Options: dhcpv4.OptionsFromList(
					dhcpv4.OptMessageType(dhcpv4.MessageTypeRequest),
				),
			},
			wantErr: errBadBackend,
		},
		"request release type": {
			server: Handler{
				Backend: &mockBackend{err: errBadBackend},
				IPAddr:  netip.MustParseAddr("127.0.0.1"),
			},
			req: &dhcpv4.DHCPv4{
				OpCode:       dhcpv4.OpcodeBootRequest,
				ClientHWAddr: []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
				Options: dhcpv4.OptionsFromList(
					dhcpv4.OptMessageType(dhcpv4.MessageTypeRelease),
				),
			},
			wantErr: errBadBackend,
		},
		"unknown message type": {
			server: Handler{
				Backend: &mockBackend{err: errBadBackend},
				IPAddr:  netip.MustParseAddr("127.0.0.1"),
			},
			req: &dhcpv4.DHCPv4{
				OpCode:       dhcpv4.OpcodeBootRequest,
				ClientHWAddr: []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
				Options: dhcpv4.OptionsFromList(
					dhcpv4.OptMessageType(dhcpv4.MessageTypeInform),
				),
			},
			wantErr: errBadBackend,
		},
		"fail WriteTo": {
			server: Handler{
				Backend: &mockBackend{},
			},
			req: &dhcpv4.DHCPv4{
				OpCode:       dhcpv4.OpcodeBootRequest,
				ClientHWAddr: []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
				Options: dhcpv4.OptionsFromList(
					dhcpv4.OptMessageType(dhcpv4.MessageTypeDiscover),
				),
			},
			wantErr: errBadBackend,
			nilPeer: true,
		},
		"nil incoming packet": {
			want:    nil,
			wantErr: errBadBackend,
		},
		/*"autoregister missing hw": {
			server: Handler{
				Backend:      &mockBackend{err: errMacNotFound([]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x05}, "hw not found")},
				IPAddr:       netip.MustParseAddr("127.0.0.1"),
				AutoRegister: true,
			},
			req: &dhcpv4.DHCPv4{
				OpCode:       dhcpv4.OpcodeBootRequest,
				ClientHWAddr: []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x05},
				Options: dhcpv4.OptionsFromList(
					dhcpv4.OptMessageType(dhcpv4.MessageTypeDiscover),
				),
			},
			want: &dhcpv4.DHCPv4{
				OpCode:        dhcpv4.OpcodeBootReply,
				ClientHWAddr:  []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x04},
				ClientIPAddr:  []byte{0, 0, 0, 0},
				YourIPAddr:    []byte{192, 168, 1, 100},
				ServerIPAddr:  []byte{127, 0, 0, 1},
				GatewayIPAddr: []byte{0, 0, 0, 0},
				Options: dhcpv4.OptionsFromList(
					dhcpv4.OptMessageType(dhcpv4.MessageTypeOffer),
					dhcpv4.OptServerIdentifier(net.IP{127, 0, 0, 1}),
					dhcpv4.OptIPAddressLeaseTime(time.Minute),
					dhcpv4.OptSubnetMask(net.IPMask(net.IP{255, 255, 255, 0}.To4())),
					dhcpv4.OptRouter([]net.IP{{192, 168, 1, 1}}...),
					dhcpv4.OptDNS([]net.IP{{1, 1, 1, 1}}...),
					dhcpv4.OptDomainName("mydomain.com"),
					dhcpv4.OptHostName("test-host"),
					dhcpv4.OptBroadcastAddress(net.IP{192, 168, 1, 255}),
					dhcpv4.OptNTPServers([]net.IP{{132, 163, 96, 2}}...),
					dhcpv4.OptDomainSearch(&rfc1035label.Labels{Labels: []string{"mydomain.com"}}),
				),
			},
		},*/
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			s := tt.server
			conn, err := nettest.NewLocalPacketListener("udp")
			if err != nil {
				t.Fatal("1", err)
			}
			defer conn.Close()

			pc, err := net.ListenPacket("udp4", ":0")
			if err != nil {
				t.Fatal("2", err)
			}
			defer pc.Close()
			peer := &net.UDPAddr{IP: net.IP{127, 0, 0, 1}, Port: pc.LocalAddr().(*net.UDPAddr).Port}
			if tt.nilPeer {
				peer = nil
			}
			s.Handle(conn, peer, tt.req)

			msg, err := client(pc)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("client() error = %v, wantErr %v", err, tt.wantErr)
			}

			if diff := cmp.Diff(tt.want, msg, cmpopts.IgnoreUnexported(dhcpv4.DHCPv4{})); diff != "" {
				t.Fatal("diff", diff)
			}
		})
	}
}

func client(pc net.PacketConn) (*dhcpv4.DHCPv4, error) {
	buf := make([]byte, 1024)
	pc.SetReadDeadline(time.Now().Add(time.Millisecond * 100))
	if _, _, err := pc.ReadFrom(buf); err != nil {
		return nil, errBadBackend
	}
	msg, err := dhcpv4.FromBytes(buf)
	if err != nil {
		return nil, errBadBackend
	}

	return msg, nil
}

func TestUpdateMsg(t *testing.T) {
	type args struct {
		m       *dhcpv4.DHCPv4
		data    *data.DHCP
		netboot *data.Netboot
		msg     dhcpv4.MessageType
	}
	tests := map[string]struct {
		args    args
		want    *dhcpv4.DHCPv4
		wantErr bool
	}{
		"success": {
			args: args{
				m: &dhcpv4.DHCPv4{
					OpCode:       dhcpv4.OpcodeBootRequest,
					ClientHWAddr: []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
					Options: dhcpv4.OptionsFromList(
						dhcpv4.OptUserClass("Tinkerbell"),
						dhcpv4.OptClassIdentifier("HTTPClient"),
						dhcpv4.OptClientArch(iana.EFI_ARM64_HTTP),
						dhcpv4.OptGeneric(dhcpv4.OptionClientNetworkInterfaceIdentifier, []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06}),
						dhcpv4.OptGeneric(dhcpv4.OptionClientMachineIdentifier, []byte{0x00, 0x02, 0x03, 0x04, 0x05, 0x06, 0x00, 0x02, 0x03, 0x04, 0x05, 0x06, 0x00, 0x02, 0x03, 0x04, 0x05}),
						dhcpv4.OptMessageType(dhcpv4.MessageTypeDiscover),
					),
				},
				data:    &data.DHCP{IPAddress: netip.MustParseAddr("192.168.1.100"), SubnetMask: net.IPMask(net.IP{255, 255, 255, 0}.To4())},
				netboot: &data.Netboot{AllowNetboot: true, IPXEScriptURL: &url.URL{Scheme: "http", Host: "localhost:8181", Path: "auto.ipxe"}},
				msg:     dhcpv4.MessageTypeDiscover,
			},
			want: &dhcpv4.DHCPv4{
				OpCode:       dhcpv4.OpcodeBootReply,
				ClientHWAddr: []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
				YourIPAddr:   []byte{192, 168, 1, 100},
				ClientIPAddr: []byte{0, 0, 0, 0},
				BootFileName: "http://localhost:8181/auto.ipxe",
				Options: dhcpv4.OptionsFromList(
					dhcpv4.OptMessageType(dhcpv4.MessageTypeDiscover),
					dhcpv4.OptServerIdentifier(net.IP{127, 0, 0, 1}),
					dhcpv4.OptIPAddressLeaseTime(3600),
					dhcpv4.OptSubnetMask(net.IPMask(net.IP{255, 255, 255, 0}.To4())),
					dhcpv4.OptClassIdentifier("HTTPClient"),
					dhcpv4.OptGeneric(dhcpv4.OptionVendorSpecificInformation, dhcpv4.Options{
						6:  []byte{8},
						69: otel.TraceparentFromContext(context.Background()),
					}.ToBytes()),
				),
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			s := &Handler{
				Log:    stdr.New(log.New(os.Stdout, "", log.Lshortfile)),
				IPAddr: netip.MustParseAddr("127.0.0.1"),
				Netboot: Netboot{
					Enabled: true,
				},
				Backend: &mockBackend{
					allowNetboot: true,
					ipxeScript:   &url.URL{Scheme: "http", Host: "localhost:8181", Path: "auto.ipxe"},
				},
				// Listener: netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), 67),
			}
			got := s.updateMsg(context.Background(), tt.args.m, tt.args.data, tt.args.netboot, tt.args.msg)
			if diff := cmp.Diff(got, tt.want, cmpopts.IgnoreUnexported(dhcpv4.DHCPv4{})); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

func TestOne(t *testing.T) {
	t.Skip()
	h := &Handler{}
	_, _, err := h.readBackend(context.Background(), nil)
	t.Fatal(err)
}

func TestReadBackend(t *testing.T) {
	tests := map[string]struct {
		input       *dhcpv4.DHCPv4
		wantDHCP    *data.DHCP
		wantNetboot *data.Netboot
		wantErr     error
	}{
		"success": {
			input: &dhcpv4.DHCPv4{
				OpCode:       dhcpv4.OpcodeBootRequest,
				ClientHWAddr: []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
				Options: dhcpv4.OptionsFromList(
					dhcpv4.OptUserClass("Tinkerbell"),
					dhcpv4.OptClassIdentifier("HTTPClient"),
					dhcpv4.OptClientArch(iana.EFI_ARM64_HTTP),
					dhcpv4.OptGeneric(dhcpv4.OptionClientNetworkInterfaceIdentifier, []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06}),
					dhcpv4.OptGeneric(dhcpv4.OptionClientMachineIdentifier, []byte{0x00, 0x02, 0x03, 0x04, 0x05, 0x06, 0x00, 0x02, 0x03, 0x04, 0x05, 0x06, 0x00, 0x02, 0x03, 0x04, 0x05}),
					dhcpv4.OptMessageType(dhcpv4.MessageTypeDiscover),
				),
			},
			wantDHCP: &data.DHCP{
				MACAddress:       []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
				IPAddress:        netip.MustParseAddr("192.168.1.100"),
				SubnetMask:       []byte{255, 255, 255, 0},
				DefaultGateway:   netip.MustParseAddr("192.168.1.1"),
				NameServers:      []net.IP{{1, 1, 1, 1}},
				Hostname:         "test-host",
				DomainName:       "mydomain.com",
				BroadcastAddress: netip.MustParseAddr("192.168.1.255"),
				NTPServers:       []net.IP{{132, 163, 96, 2}},
				LeaseTime:        60,
				DomainSearch:     []string{"mydomain.com"},
			},
			wantNetboot: &data.Netboot{AllowNetboot: true, IPXEScriptURL: &url.URL{Scheme: "http", Host: "localhost:8181", Path: "auto.ipxe"}},
			wantErr:     nil,
		},
		"failure": {
			input:   &dhcpv4.DHCPv4{},
			wantErr: errBadBackend,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			s := &Handler{
				Log:    stdr.New(log.New(os.Stdout, "", log.Lshortfile)),
				IPAddr: netip.MustParseAddr("127.0.0.1"),
				Netboot: Netboot{
					Enabled: true,
				},
				Backend: &mockBackend{
					err:          tt.wantErr,
					allowNetboot: true,
					ipxeScript:   &url.URL{Scheme: "http", Host: "localhost:8181", Path: "auto.ipxe"},
				},
				// Listener: netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), 67),
			}
			netaddrComparer := cmp.Comparer(func(x, y netip.Addr) bool {
				i := x.Compare(y)
				return i == 0
			})
			gotDHCP, gotNetboot, err := s.readBackend(context.Background(), tt.input.ClientHWAddr)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("gotErr: %v, wantErr: %v", err, tt.wantErr)
			}
			if diff := cmp.Diff(gotDHCP, tt.wantDHCP, netaddrComparer); diff != "" {
				t.Fatal(diff)
			}
			if diff := cmp.Diff(gotNetboot, tt.wantNetboot); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

func TestIsNetbootClient(t *testing.T) {
	tests := map[string]struct {
		input *dhcpv4.DHCPv4
		want  bool
	}{
		"fail invalid message type": {input: &dhcpv4.DHCPv4{Options: dhcpv4.OptionsFromList(dhcpv4.OptMessageType(dhcpv4.MessageTypeInform))}, want: false},
		"fail no opt60":             {input: &dhcpv4.DHCPv4{Options: dhcpv4.OptionsFromList(dhcpv4.OptMessageType(dhcpv4.MessageTypeDiscover))}, want: false},
		"fail bad opt60": {input: &dhcpv4.DHCPv4{Options: dhcpv4.OptionsFromList(
			dhcpv4.OptMessageType(dhcpv4.MessageTypeDiscover),
			dhcpv4.OptClassIdentifier("BadClient"),
		)}, want: false},
		"fail no opt93": {input: &dhcpv4.DHCPv4{Options: dhcpv4.OptionsFromList(
			dhcpv4.OptMessageType(dhcpv4.MessageTypeDiscover),
			dhcpv4.OptClassIdentifier("HTTPClient:Arch:xxxxx:UNDI:yyyzzz"),
		)}, want: false},
		"fail no opt94": {input: &dhcpv4.DHCPv4{Options: dhcpv4.OptionsFromList(
			dhcpv4.OptMessageType(dhcpv4.MessageTypeDiscover),
			dhcpv4.OptClassIdentifier("HTTPClient:Arch:xxxxx:UNDI:yyyzzz"),
			dhcpv4.OptClientArch(iana.EFI_ARM64_HTTP),
		)}, want: false},
		"fail invalid opt97[0] != 0": {input: &dhcpv4.DHCPv4{Options: dhcpv4.OptionsFromList(
			dhcpv4.OptMessageType(dhcpv4.MessageTypeDiscover),
			dhcpv4.OptClassIdentifier("HTTPClient:Arch:xxxxx:UNDI:yyyzzz"),
			dhcpv4.OptClientArch(iana.EFI_ARM64_HTTP),
			dhcpv4.OptGeneric(dhcpv4.OptionClientNetworkInterfaceIdentifier, []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06}),
			dhcpv4.OptGeneric(dhcpv4.OptionClientMachineIdentifier, []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x00, 0x02, 0x03, 0x04, 0x05, 0x06, 0x00, 0x02, 0x03, 0x04, 0x05}),
		)}, want: false},
		"fail invalid len(opt97)": {input: &dhcpv4.DHCPv4{Options: dhcpv4.OptionsFromList(
			dhcpv4.OptMessageType(dhcpv4.MessageTypeDiscover),
			dhcpv4.OptClassIdentifier("HTTPClient:Arch:xxxxx:UNDI:yyyzzz"),
			dhcpv4.OptClientArch(iana.EFI_ARM64_HTTP),
			dhcpv4.OptGeneric(dhcpv4.OptionClientNetworkInterfaceIdentifier, []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06}),
			dhcpv4.OptGeneric(dhcpv4.OptionClientMachineIdentifier, []byte{0x01, 0x02}),
		)}, want: false},
		"success len(opt97) == 0": {input: &dhcpv4.DHCPv4{Options: dhcpv4.OptionsFromList(
			dhcpv4.OptMessageType(dhcpv4.MessageTypeDiscover),
			dhcpv4.OptClassIdentifier("HTTPClient:Arch:xxxxx:UNDI:yyyzzz"),
			dhcpv4.OptClientArch(iana.EFI_ARM64_HTTP),
			dhcpv4.OptGeneric(dhcpv4.OptionClientNetworkInterfaceIdentifier, []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06}),
			dhcpv4.OptGeneric(dhcpv4.OptionClientMachineIdentifier, []byte{}),
		)}, want: true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			s := &Handler{Log: logr.Discard()}
			if s.isNetbootClient(tt.input) != tt.want {
				t.Errorf("isNetbootClient() = %v, want %v", !tt.want, tt.want)
			}
		})
	}
}

func TestEncodeToAttributes(t *testing.T) {
	tests := map[string]struct {
		input   *dhcpv4.DHCPv4
		want    []attribute.KeyValue
		wantErr error
	}{
		"success": {
			input: &dhcpv4.DHCPv4{BootFileName: "snp.efi"},
			want: []attribute.KeyValue{
				attribute.String("DHCP.testing.Header.file", "snp.efi"),
				attribute.String("DHCP.testing.Header.flags", "Unicast"),
			},
		},
		"error": {},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			stdr.SetVerbosity(1)
			s := &Handler{Log: stdr.New(log.New(os.Stdout, "", log.Lshortfile))}
			kvs := s.encodeToAttributes(tt.input, "testing")
			got := attribute.NewSet(kvs...)
			want := attribute.NewSet(tt.want...)
			enc := attribute.DefaultEncoder()
			if diff := cmp.Diff(got.Encoded(enc), want.Encoded(enc)); diff != "" {
				t.Log(got.Encoded(enc))
				t.Log(want.Encoded(enc))
				t.Fatal(diff)
			}
		})
	}
}
