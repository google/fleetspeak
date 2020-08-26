package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net"
	"os"
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

func renderProxyV1Header(srcAddr, dstAddr string) ([]byte, error) {
	srcColPos := strings.Index(srcAddr, ":")
	if srcColPos == -1 {
		return nil, fmt.Errorf("Failed to parse source (client) address")
	}
	dstColPos := strings.Index(dstAddr, ":")
	if dstColPos == -1 {
		return nil, fmt.Errorf("Failed to parse destination (server) address")
	}
	return []byte(fmt.Sprintf("PROXY TCP4 %v %v %v %v\r\n", srcAddr[:srcColPos], dstAddr[:dstColPos], srcAddr[srcColPos+1:len(srcAddr)], dstAddr[dstColPos+1:len(dstAddr)])), nil
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
		firstMessage, err := renderProxyV1Header(lbConn.RemoteAddr().String(), serverAddr)
		if err != nil {
			return fmt.Errorf("Failed to render Proxy header in load balancer: %v", err)
		}
		_, err = serverConn.Write(firstMessage)
		if err != nil {
			return fmt.Errorf("Failed to write Proxy header: %v", err)
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
