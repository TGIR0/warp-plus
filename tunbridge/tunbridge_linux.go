//go:build linux || android

package tunbridge

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"time"

	"github.com/bepass-org/warp-plus/wireguard/tun/netstack"
	"github.com/sagernet/gvisor/pkg/tcpip"
	"github.com/sagernet/gvisor/pkg/tcpip/adapters/gonet"
	"github.com/sagernet/gvisor/pkg/tcpip/header"
	"github.com/sagernet/gvisor/pkg/tcpip/link/fdbased"
	"github.com/sagernet/gvisor/pkg/tcpip/network/ipv4"
	"github.com/sagernet/gvisor/pkg/tcpip/network/ipv6"
	"github.com/sagernet/gvisor/pkg/tcpip/stack"
	"github.com/sagernet/gvisor/pkg/tcpip/transport/icmp"
	"github.com/sagernet/gvisor/pkg/tcpip/transport/tcp"
	"github.com/sagernet/gvisor/pkg/tcpip/transport/udp"
	"github.com/sagernet/gvisor/pkg/waiter"
)

const nicID = 1

// Start initializes a gvisor stack on the given TUN FD and bridges it to tnet.
func Start(ctx context.Context, l *slog.Logger, tunFd int, mtu int, tnet *netstack.Net) error {
	s := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol, ipv6.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol, icmp.NewProtocol4, icmp.NewProtocol6},
	})

	// Set SACK
	sackEnabledOpt := tcpip.TCPSACKEnabled(true)
	if err := s.SetTransportProtocolOption(tcp.ProtocolNumber, &sackEnabledOpt); err != nil {
		return fmt.Errorf("could not set SACK: %v", err)
	}

	// Create NIC
	linkEP, err := fdbased.New(&fdbased.Options{
		FDs:                []int{tunFd},
		MTU:                uint32(mtu),
		EthernetHeader:     false,
		Address:            "",
		PacketDispatchMode: fdbased.Readv,
	})
	if err != nil {
		return fmt.Errorf("failed to create endpoint: %v", err)
	}

	if err := s.CreateNIC(nicID, linkEP); err != nil {
		return fmt.Errorf("failed to create NIC: %v", err)
	}

	if err := s.SetPromiscuousMode(nicID, true); err != nil {
		return fmt.Errorf("failed to set promiscuous mode: %v", err)
	}
	if err := s.SetSpoofing(nicID, true); err != nil {
		return fmt.Errorf("failed to set spoofing: %v", err)
	}

	s.AddRoute(tcpip.Route{Destination: header.IPv4EmptySubnet, NIC: nicID})
	s.AddRoute(tcpip.Route{Destination: header.IPv6EmptySubnet, NIC: nicID})

	// TCP Forwarder
	tcpForwarder := tcp.NewForwarder(s, 0, 10000, func(r *tcp.ForwarderRequest) {
		var wq waiter.Queue
		ep, err := r.CreateEndpoint(&wq)
		if err != nil {
			l.Error("tcp forwarder: create endpoint failed", "error", err)
			r.Complete(true)
			return
		}
		r.Complete(false)

		// Use a goroutine to handle the connection
		go func() {
			// Create gonet conn for the VPN side
			conn := gonet.NewTCPConn(&wq, ep)
			defer conn.Close()

			// Dial the target using tnet
			dstAddr := r.ID().LocalAddress // The destination address from the packet
			dstPort := r.ID().LocalPort

			targetStr := net.JoinHostPort(dstAddr.String(), fmt.Sprintf("%d", dstPort))
			l.Debug("tcp forward", "target", targetStr)

			targetConn, err := tnet.Dial("tcp", targetStr)
			if err != nil {
				l.Error("tcp dial failed", "target", targetStr, "error", err)
				return
			}
			defer targetConn.Close()

			// Bridge
			copyConn(conn, targetConn)
		}()
	})
	s.SetTransportProtocolHandler(tcp.ProtocolNumber, tcpForwarder.HandlePacket)

	// UDP Forwarder
	udpForwarder := udp.NewForwarder(s, func(r *udp.ForwarderRequest) bool {
		var wq waiter.Queue
		ep, err := r.CreateEndpoint(&wq)
		if err != nil {
			l.Error("udp forwarder: create endpoint failed", "error", err)
			return true
		}

		go func() {
			conn := gonet.NewUDPConn(&wq, ep)
			defer conn.Close()

			dstAddr := r.ID().LocalAddress
			dstPort := r.ID().LocalPort
			targetStr := net.JoinHostPort(dstAddr.String(), fmt.Sprintf("%d", dstPort))

			// For UDP, we need to establish a session or just send packets.
			// tnet.Dial("udp", ...) returns a conn that is connected to the target.

			targetConn, err := tnet.Dial("udp", targetStr)
			if err != nil {
				l.Error("udp dial failed", "target", targetStr, "error", err)
				return
			}
			defer targetConn.Close()

			// Set deadlines to cleanup idle UDP
			conn.SetDeadline(time.Now().Add(30 * time.Second))
			targetConn.SetDeadline(time.Now().Add(30 * time.Second))

			copyConn(conn, targetConn)
		}()

		return true
	})
	s.SetTransportProtocolHandler(udp.ProtocolNumber, udpForwarder.HandlePacket)

	l.Info("tunbridge started")
	<-ctx.Done()

	s.RemoveNIC(nicID)
	s.Close()
	return nil
}

func copyConn(a, b net.Conn) {
	done := make(chan struct{}, 2)
	go func() {
		io.Copy(a, b)
		done <- struct{}{}
	}()
	go func() {
		io.Copy(b, a)
		done <- struct{}{}
	}()
	<-done
}
