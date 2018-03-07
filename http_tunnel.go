// This file provides a dialer type of "http://" scheme for
// golang.org/x/net/proxy package.
//
// The dialer type will be automatically registered by init().
//
// The dialer requests an upstream HTTP proxy to create a TCP tunnel
// by CONNECT method.

package transproxy

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/net/proxy"
)

func init() {
	proxy.RegisterDialerType("http", httpDialType)
}

type httpDialer struct {
	addr    string
	header  http.Header
	forward proxy.Dialer
}

func httpDialType(u *url.URL, forward proxy.Dialer) (proxy.Dialer, error) {
	var header http.Header
	if uu := u.User; uu != nil {
		passwd, _ := uu.Password()
		up := uu.Username() + ":" + passwd
		authz := "Basic " + base64.StdEncoding.EncodeToString([]byte(up))
		header = map[string][]string{
			"Proxy-Authorization": {authz},
		}
	}
	return &httpDialer{
		addr:    u.Host,
		header:  header,
		forward: forward,
	}, nil
}

func (d *httpDialer) Dial(network, addr string) (c net.Conn, err error) {
	req := &http.Request{
		Method: "CONNECT",
		URL:    &url.URL{Opaque: addr},
		Host:   addr,
		Header: d.header,
	}
	c, err = d.forward.Dial("tcp", d.addr)
	if err != nil {
		return
	}
	req.Write(c)

	// Read response until "\r\n\r\n".
	// bufio cannot be used as the connected server may not be
	// a HTTP(S) server.
	c.SetReadDeadline(time.Now().Add(10 * time.Second))
	buf := make([]byte, 0, 4096)
	b := make([]byte, 1)
	state := 0
	for {
		_, e := c.Read(b)
		if e != nil {
			c.Close()
			return nil, errors.New("reset proxy connection")
		}
		buf = append(buf, b[0])
		switch state {
		case 0:
			if b[0] == byte('\r') {
				state++
			}
			continue
		case 1:
			if b[0] == byte('\n') {
				state++
			} else {
				state = 0
			}
			continue
		case 2:
			if b[0] == byte('\r') {
				state++
			} else {
				state = 0
			}
			continue
		case 3:
			if b[0] == byte('\n') {
				goto PARSE
			} else {
				state = 0
			}
		}
	}

PARSE:
	var zero time.Time
	c.SetReadDeadline(zero)
	resp, e := http.ReadResponse(bufio.NewReader(bytes.NewBuffer(buf)), req)
	if e != nil {
		c.Close()
		return nil, e
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		c.Close()
		return nil, fmt.Errorf("proxy returns %s", resp.Status)
	}

	return c, nil
}
