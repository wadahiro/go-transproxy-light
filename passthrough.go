package transproxy

import (
	"io"
	"log"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"
)

type PassThroughProxy struct {
	PassThroughProxyConfig
}

type PassThroughProxyConfig struct {
	ListenAddress string
	NoProxy       NoProxy
	DNSProxy      *DNSProxy
}

func NewPassThroughProxy(c PassThroughProxyConfig) *PassThroughProxy {
	return &PassThroughProxy{
		PassThroughProxyConfig: c,
	}
}

func (s *PassThroughProxy) GetType() string {
	return "PassThrough-Proxy"
}

func (s *PassThroughProxy) GetListenPort() int {
	array := strings.Split(s.ListenAddress, ":")
	i, _ := strconv.Atoi(array[1])
	return i
}

func (s *PassThroughProxy) Start() error {
	//pdialer := proxy.FromEnvironment()

	dialer := &net.Dialer{
		KeepAlive: 3 * time.Minute,
		DualStack: true,
	}
	u, err := url.Parse(os.Getenv("http_proxy"))
	if err != nil {
		return err
	}

	pdialer, err := proxy.FromURL(u, dialer)
	if err != nil {
		return err
	}

	log.Printf("info: Start listener on %s category='%s'", s.GetType(), s.ListenAddress)

	l, err := net.Listen("tcp", s.ListenAddress)
	if err != nil {
		return err
	}

	go func() {
		for {
			conn, err := l.Accept() // wait here
			if err != nil {
				log.Printf("warn: category='%s' Error accepting new connection - %s", s.GetType(), err.Error())
				return
			}

			log.Printf("debug: category='%s' Accepted new connection", s.GetType())

			go func(conn net.Conn) {
				// access logging
				localAddr := conn.LocalAddr().String()
				localHost, localPort, _ := net.SplitHostPort(localAddr)
				remoteAddr := conn.RemoteAddr().String()

				hostName, err := s.DNSProxy.ReverseLookup(localHost)
				if err != nil {
					log.Printf("error: category='%s' remoteAddr='%s' localAddr='%s' Can't resolve localAddr", s.GetType(), remoteAddr, localAddr)
					conn.Close()
					return
				}
				log.Printf("info: category='%s' remoteAddr='%s' localAddr='%s' resolvedHostName='%s'", s.GetType(), remoteAddr, localAddr, hostName)

				destConn, err := pdialer.Dial("tcp", hostName+":"+localPort)
				if err != nil {
					log.Printf("error: category='%s' remoteAddr='%s' localAddr='%s' hostName='%s:%s' Can't connect: %s", s.GetType(), remoteAddr, localAddr, hostName, localPort, err.Error())
					conn.Close()
					return
				}

				wg := &sync.WaitGroup{}
				wg.Add(1)
				go transfer(wg, destConn, conn)
				wg.Add(1)
				go transfer(wg, conn, destConn)
				wg.Wait()
			}(conn)
		}
	}()

	return nil
}

func (s *PassThroughProxy) Stop() {
}

func transfer(wg *sync.WaitGroup, destination io.WriteCloser, source io.ReadCloser) {
	defer destination.Close()
	defer source.Close()
	defer wg.Done()
	io.Copy(destination, source)
}
