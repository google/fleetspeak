package proxyproto

import (
	"fmt"
	proxyproto "github.com/pires/go-proxyproto"
	"io"
	"net"
	"strconv"
)

func splitHostPort(addr string) (net.IP, uint16, error) {
	ta, err := net.ResolveTCPAddr("tcp", addr)
	if err == nil {
		return ta.IP, uint16(ta.Port), nil
	}
	hostStr, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, 0, err
	}
	host := net.ParseIP(hostStr)
	if host == nil {
		return nil, 0, fmt.Errorf("Failed to parse IP")
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, 0, fmt.Errorf("Failed to parse port: %v", err)
	}
	return host, uint16(port), nil
}

func WriteFirstProxyMessage(w io.Writer, srcAddr, dstAddr string) error {
	srcHost, srcPort, err := splitHostPort(srcAddr)
	if err != nil {
		return fmt.Errorf("Failed to parse source (client) address: %v", err)
	}
	dstHost, dstPort, err := splitHostPort(dstAddr)
	if err != nil {
		return fmt.Errorf("Failed to parse destination (server) address: %v", err)
	}
	header := proxyproto.Header{
		Version:           1,
		TransportProtocol: proxyproto.TCPv4,
		SourceAddr:        &net.TCPAddr{IP: srcHost, Port: int(srcPort)},
		DestinationAddr:   &net.TCPAddr{IP: dstHost, Port: int(dstPort)},
	}
	_, err = header.WriteTo(w)
	if err != nil {
		return fmt.Errorf("Failed to write Proxy header: %v", err)
	}
	return nil
}
