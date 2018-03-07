package transproxy

import (
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

type DNSProxy struct {
	DNSProxyConfig
	udpServer *dns.Server
	tcpServer *dns.Server
	udpClient *dns.Client // used for fowarding to internal DNS
	tcpClient *dns.Client // used for fowarding to internal DNS

	lock         sync.Mutex
	currentIP    uint32
	startIP      uint32
	endIP        uint32
	ipMap        map[string]uint32
	ipReverseMap map[uint32]string
}

type DNSProxyConfig struct {
	DNSListenAddress string
	DNSEnableUDP     bool
	DNSEnableTCP     bool
	PrivateDNS       string
	NoProxyZones     []string
	StartLocalIP     string
	EndLocalIP       string
}

func NewDNSProxy(c DNSProxyConfig) *DNSProxy {

	// fix dns address
	if c.PrivateDNS != "" {
		_, _, err := net.SplitHostPort(c.PrivateDNS)
		if err != nil {
			c.PrivateDNS = net.JoinHostPort(c.PrivateDNS, "53")
		}
	}

	// fix domains for DNS noproxy zones
	var dnsNoProxyZones []string
	for _, s := range c.NoProxyZones {
		if !strings.HasSuffix(s, ".") {
			s += "."
		}
		dnsNoProxyZones = append(dnsNoProxyZones, s)
	}
	c.NoProxyZones = dnsNoProxyZones

	return &DNSProxy{
		DNSProxyConfig: c,
		udpServer:      nil,
		tcpServer:      nil,
		udpClient: &dns.Client{
			Net:            "udp",
			Timeout:        time.Duration(10) * time.Second,
			SingleInflight: true,
		},
		tcpClient: &dns.Client{
			Net:            "tcp",
			Timeout:        time.Duration(10) * time.Second,
			SingleInflight: true,
		},
		currentIP:    ip2int(net.ParseIP(c.StartLocalIP).To4()),
		startIP:      ip2int(net.ParseIP(c.StartLocalIP).To4()),
		endIP:        ip2int(net.ParseIP(c.EndLocalIP).To4()),
		ipMap:        make(map[string]uint32),
		ipReverseMap: make(map[uint32]string),
	}
}

func (s *DNSProxy) NextIP(domain string) string {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.currentIP += uint32(1)
	if s.currentIP > s.endIP {
		s.currentIP = s.startIP
	}
	s.ipMap[domain] = s.currentIP
	s.ipReverseMap[s.currentIP] = domain

	return int2ip(s.currentIP).String()
}

func (s *DNSProxy) Lookup(domain string) (string, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	v, ok := s.ipMap[domain]
	if !ok {
		return "", errors.New(fmt.Sprintf("Not found %s in the DNS cache", domain))
	}
	return int2ip(v).String(), nil
}

func (s *DNSProxy) ReverseLookup(ip string) (string, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	v, ok := s.ipReverseMap[ip2int(net.ParseIP(ip).To4())]
	if !ok {
		return "", errors.New(fmt.Sprintf("Not found %s in the reverse DNS cache", ip))
	}
	return v, nil
}

func (s *DNSProxy) Start() error {
	log.Printf("info: Start listener on %s category='DNS-Proxy'", s.DNSListenAddress)

	// Setup DNS Handler
	dnsHandle := func(w dns.ResponseWriter, req *dns.Msg) {
		if len(req.Question) == 0 {
			dns.HandleFailed(w, req)
			return
		}

		// access logging
		host, _, _ := net.SplitHostPort(w.RemoteAddr().String())
		log.Printf("info: category='DNS-Proxy' remoteAddr='%s' questionName='%s' questionType='%s'", host, req.Question[0].Name, dns.TypeToString[req.Question[0].Qtype])

		// Resolve by proxied private DNS
		for _, domain := range s.NoProxyZones {
			log.Printf("debug: category='DNS-Proxy' Checking DNS route, request: %s, no_proxy: %s", req.Question[0].Name, domain)
			if strings.HasSuffix(req.Question[0].Name, domain) {
				log.Printf("debug: category='DNS-Proxy' Matched! Routing to private DNS, request: %s, no_proxy: %s", req.Question[0].Name, domain)
				s.handlePrivate(w, req)
				return
			}
		}

		// Resolve self
		s.handlePublic(w, req)
	}

	dns.HandleFunc(".", dnsHandle)

	// Start DNS Server
	if s.DNSEnableUDP {
		s.udpServer = &dns.Server{
			Addr:       s.DNSListenAddress,
			Net:        "udp",
			TsigSecret: nil,
		}
	}
	if s.DNSEnableTCP {
		s.tcpServer = &dns.Server{
			Addr:       s.DNSListenAddress,
			Net:        "tcp",
			TsigSecret: nil,
		}
	}

	go func() {
		if s.udpServer != nil {
			if err := s.udpServer.ListenAndServe(); err != nil {
				log.Fatal("alert: category='DNS-Proxy' %s", err.Error())
			}
		}
		if s.tcpServer != nil {
			if err := s.tcpServer.ListenAndServe(); err != nil {
				log.Fatal("alert: category='DNS-Proxy' %s", err.Error())
			}
		}
	}()

	return nil
}

func (s *DNSProxy) handlePublic(w dns.ResponseWriter, req *dns.Msg) {
	log.Printf("debug: category='DNS-Proxy' DNS request. %#v, %s", req, req)

	nextIP, err := s.Lookup(req.Question[0].Name)
	if err != nil {
		nextIP = s.NextIP(req.Question[0].Name)
	}

	// Reply response with 127.0.0.1 always for proxy
	rr, err := dns.NewRR(fmt.Sprintf("%s 60 IN A %s", req.Question[0].Name, nextIP))
	if err != nil {
		log.Printf("error: category='DNS-Proxy' DNS response failed. %s, %#v, %s", err.Error(), req, req)
		dns.HandleFailed(w, req)
		return
	}
	m := new(dns.Msg)
	m.SetReply(req)
	m.Authoritative, m.RecursionAvailable, m.Compress = true, true, true
	m.Answer = []dns.RR{rr}
	m.Rcode = dns.RcodeSuccess

	w.WriteMsg(m)
}

func (s *DNSProxy) handlePrivate(w dns.ResponseWriter, req *dns.Msg) {
	var c *dns.Client
	if _, ok := w.RemoteAddr().(*net.TCPAddr); ok {
		c = s.tcpClient
	} else {
		c = s.udpClient
	}

	log.Printf("debug: category='DNS-Proxy' DNS request. %#v, %s", req, req)

	resp, _, err := c.Exchange(req, s.PrivateDNS)
	if err != nil {
		log.Printf("warn: category='DNS-Proxy' DNS Client failed. %s, %#v, %s", err.Error(), req, req)
		dns.HandleFailed(w, req)
		return
	}
	w.WriteMsg(resp)
}

func (s *DNSProxy) Stop() {
	log.Printf("info: category='DNS-Proxy' Shutting down DNS service on interrupt\n")

	if s.udpServer != nil {
		if err := s.udpServer.Shutdown(); err != nil {
			log.Printf("error: category='DNS-Proxy' %s", err.Error())
		}
		s.udpServer = nil
	}
	if s.tcpServer != nil {
		if err := s.tcpServer.Shutdown(); err != nil {
			log.Printf("error: category='DNS-Proxy' %s", err.Error())
		}
		s.tcpServer = nil
	}
}

func ip2int(ip net.IP) uint32 {
	if len(ip) == 16 {
		return binary.BigEndian.Uint32(ip[12:16])
	}
	return binary.BigEndian.Uint32(ip)
}

func int2ip(nn uint32) net.IP {
	ip := make(net.IP, 4)
	binary.BigEndian.PutUint32(ip, nn)
	return ip
}
