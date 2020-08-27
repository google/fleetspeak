package main

import (
	"flag"
	"fmt"
	proxyproto "github.com/pires/go-proxyproto"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

var (
	serversFile        = flag.String("servers_file", "", "File with server hosts")
	serverFrontendAddr = flag.String("frontend_address", "", "Frontend address for clients to connect")
)

func copy(wc io.WriteCloser, r io.Reader) {
	defer wc.Close()
	io.Copy(wc, r)
}

func splitHostPort(addr string) (net.IP, uint16, error) {
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

func writeFirstProxyMessage(w io.Writer, srcAddr, dstAddr string) error {
	srcHost, srcPort, err := splitHostPort(srcAddr)
	if err != nil {
		return fmt.Errorf("Failed to parse source (client) address: %v", err)
	}
	dstHost, dstPort, err := splitHostPort(dstAddr)
	if err != nil {
		return fmt.Errorf("Failed to parse destination (server) address: %v", err)
	}
	header := proxyproto.Header{
		Version:            1,
		TransportProtocol:  proxyproto.TCPv4,
		SourceAddress:      srcHost,
		DestinationAddress: dstHost,
		SourcePort:         srcPort,
		DestinationPort:    dstPort,
	}
	_, err = header.WriteTo(w)
	if err != nil {
		return fmt.Errorf("Failed to write Proxy header: %v", err)
	}
	return nil
}

func run() error {
	dat, err := ioutil.ReadFile(*serversFile)
	if err != nil {
		return fmt.Errorf("Failed to read serversFile: %v", err)
	}
	serverHosts := strings.Fields(string(dat))
	if len(serverHosts) == 0 {
		return fmt.Errorf("No server hosts were provided")
	}

	ln, err := net.Listen("tcp", *serverFrontendAddr)
	if err != nil {
		return fmt.Errorf("Failed to bind: %v", err)
	}
	log.Printf("Load balancer started on %v\n", *serverFrontendAddr)

	for {
		lbConn, err := ln.Accept()
		if err != nil {
			return fmt.Errorf("Failed to accept connection: %v", err)
		}
		var serverAddr string
		var serverConn net.Conn
		retriesLeft := 10
		for {
			serverAddr = serverHosts[rand.Int()%len(serverHosts)]
			serverConn, err = net.Dial("tcp", serverAddr)
			if err != nil {
				log.Printf("Failed to connect to server (%v): %v, retrying...\n", serverAddr, err)
				retriesLeft--
				if retriesLeft < 0 {
					return fmt.Errorf("Maximum number of retries exceeded - no active servers were found")
				}
				time.Sleep(time.Second * 2)
			} else {
				break
			}
		}
		log.Printf("Connection accepted, server: %v\n", serverAddr)
		err = writeFirstProxyMessage(serverConn, lbConn.RemoteAddr().String(), serverAddr)
		if err != nil {
			return err
		}
		go copy(serverConn, lbConn)
		go copy(lbConn, serverConn)
	}
}

func main() {
	flag.Parse()
	err := run()
	if err != nil {
		fmt.Printf("Load balancer failed: %v\n", err)
		os.Exit(1)
	}
}
