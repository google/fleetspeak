package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net"
	"os"
	"strings"
    "time"
    "log"
)

var (
	serversFile        = flag.String("servers_file", "", "File with server hosts")
	serverFrontendAddr = flag.String("frontend_address", "", "Frontend address for clients to connect")
)

func copy(wc io.WriteCloser, r io.Reader) {
	defer wc.Close()
	io.Copy(wc, r)
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
        for {
		    serverAddr = serverHosts[rand.Int() % len(serverHosts)]
		    serverConn, err = net.Dial("tcp", serverAddr)
            if err != nil {
                log.Printf("Failed to connect to server (%v): %v, retrying...\n", serverAddr, err)
                time.Sleep(time.Second * 1)
            } else {
                break
            }
        }
        log.Printf("Connection accepted, server: %v\n", serverAddr)
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
