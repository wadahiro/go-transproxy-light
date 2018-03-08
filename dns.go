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

	dnsSettings interface{}
}

type DNSProxyConfig struct {
	DNSListenAddress string
	DNSEnableUDP     bool
	DNSEnableTCP     bool
	PrivateDNS       []string
	NoProxy          []string
	StartLocalIP     string
	EndLocalIP       string
}

func NewDNSProxy(c DNSProxyConfig) *DNSProxy {

	// fix dns address
	dnsServers := []string{}
	for _, dnsServer := range c.PrivateDNS {
		if dnsServer != "" {
			_, _, err := net.SplitHostPort(dnsServer)
			if err != nil {
				fixed := net.JoinHostPort(dnsServer, "53")
				dnsServers = append(dnsServers, fixed)
			}
		}
	}
	c.PrivateDNS = dnsServers

	// fix domains for DNS noproxy zones
	var dnsNoProxy []string
	for _, s := range c.NoProxy {
		if !strings.HasSuffix(s, ".") {
			s += "."
		}
		dnsNoProxy = append(dnsNoProxy, s)
	}
	c.NoProxy = dnsNoProxy

	log.Printf("info: NoProxyZone: %s", c.NoProxy)

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

		// Resolve by proxied private DNS
		for _, domain := range s.NoProxy {
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

	dnsServers := s.Setup()
	if len(dnsServers) > 0 && len(s.PrivateDNS) == 0 {
		log.Printf("info: category='DNS-Proxy' Use DNS servers: %s", dnsServers)
		s.PrivateDNS = dnsServers
	}

	go func() {
		if s.udpServer != nil {
			if err := s.udpServer.ListenAndServe(); err != nil {
				log.Fatal("alert: category='DNS-Proxy' %s", err)
			}
		}
		if s.tcpServer != nil {
			if err := s.tcpServer.ListenAndServe(); err != nil {
				log.Fatal("alert: category='DNS-Proxy' %s", err)
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

	// access logging
	host, _, _ := net.SplitHostPort(w.RemoteAddr().String())
	log.Printf("info: Resolved by public. category='DNS-Proxy' remoteAddr='%s' questionName='%s' questionType='%s' answer='%v'", host, req.Question[0].Name, dns.TypeToString[req.Question[0].Qtype], m.Answer)

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

	var resp *dns.Msg
	var err error
	for _, dnsServer := range s.PrivateDNS {
		resp, _, err = c.Exchange(req, dnsServer)
		if err != nil {
			log.Printf("warn: category='DNS-Proxy' DNS request to %s failed. %s, %#v, %s", dnsServer, err, req, req)
		} else {
			break
		}
	}

	if resp == nil {
		dns.HandleFailed(w, req)
		return
	}

	// access logging
	host, _, _ := net.SplitHostPort(w.RemoteAddr().String())
	if len(resp.Answer) > 0 {
		log.Printf("info: Resolved by private. category='DNS-Proxy' remoteAddr='%s' questionName='%s' questionType='%s' answer='%v'", host, req.Question[0].Name, dns.TypeToString[req.Question[0].Qtype], resp.Answer)
	} else {
		log.Printf("info: Resolved by private. category='DNS-Proxy' remoteAddr='%s' questionName='%s' questionType='%s' answer=''", host, req.Question[0].Name, dns.TypeToString[req.Question[0].Qtype])
	}

	w.WriteMsg(resp)
}

func (s *DNSProxy) Stop() {
	log.Printf("info: category='DNS-Proxy' Shutting down DNS service on interrupt\n")

	s.Teardown()

	if s.udpServer != nil {
		if err := s.udpServer.Shutdown(); err != nil {
			log.Printf("warn: category='DNS-Proxy' %s", err)
		}
		s.udpServer = nil
	}
	if s.tcpServer != nil {
		if err := s.tcpServer.Shutdown(); err != nil {
			log.Printf("warn: category='DNS-Proxy' %s", err)
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
