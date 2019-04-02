package https

import (
	"bufio"
	"fmt"
	"net"

	log "github.com/golang/glog"

	proxyproto "github.com/pires/go-proxyproto"
)

type ProxyListener struct {
	net.Listener
}

func (l *ProxyListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return c, err
	}
	r := bufio.NewReader(c)
	h, err := proxyproto.Read(r)
	if err == proxyproto.ErrNoProxyProtocol {
		log.Warningf("Received connection from %v without proxy header.", c.RemoteAddr())
		return &proxyConn{
			Conn:   c,
			reader: r,
			remote: c.RemoteAddr(),
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("error parsing proxy header: %v", err)
	}
	return &proxyConn{
		Conn:   c,
		reader: r,
		remote: &net.TCPAddr{IP: h.SourceAddress, Port: int(h.SourcePort)},
	}, nil
}

type proxyConn struct {
	net.Conn
	reader *bufio.Reader
	remote net.Addr
}

func (c *proxyConn) Read(b []byte) (int, error) {
	return c.reader.Read(b)
}

func (c *proxyConn) RemoteAddr() net.Addr {
	return c.remote
}
