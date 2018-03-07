package transproxy

import (
	"fmt"
	"log"
	"net"
)

type NoProxy struct {
	IPs     []string
	CIDRs   []*net.IPNet
	Domains []string
}

type Proxy interface {
	Start() error
	Stop()
	GetListenPort() int
	GetType() string
}

type Transproxy struct {
	TransproxyConfig
	dnsProxy *DNSProxy
	proxies  []Proxy
}

type TransproxyConfig struct {
	DNSListenAddress string
	DNSEnableUDP     bool
	DNSEnableTCP     bool
	PrivateDNS       string
	NoProxyZones     []string
	StartLocalIP     string
	EndLocalIP       string

	ProxyListenPorts []int
	NoProxy          NoProxy
}

func NewTransproxy(c TransproxyConfig) *Transproxy {
	dnsProxy := NewDNSProxy(
		DNSProxyConfig{
			DNSListenAddress: c.DNSListenAddress,
			DNSEnableUDP:     c.DNSEnableUDP,
			DNSEnableTCP:     c.DNSEnableTCP,
			PrivateDNS:       c.PrivateDNS,
			NoProxyZones:     c.NoProxy.Domains,
			StartLocalIP:     c.StartLocalIP,
			EndLocalIP:       c.EndLocalIP,
		},
	)

	proxies := []Proxy{}
	for _, p := range c.ProxyListenPorts {
		proxy := NewPassThroughProxy(
			PassThroughProxyConfig{
				ListenAddress: fmt.Sprintf(":%d", p),
				NoProxy:       c.NoProxy,
				DNSProxy:      dnsProxy,
			},
		)
		proxies = append(proxies, proxy)
	}

	return &Transproxy{
		TransproxyConfig: c,
		dnsProxy:         dnsProxy,
		proxies:          proxies,
	}
}

func (s *Transproxy) Start() error {
	for _, proxy := range s.proxies {
		if err := proxy.Start(); err != nil {
			log.Fatalf("alert: category='%s[%d]' %s", proxy.GetType(), proxy.GetListenPort(), err.Error())
		}
	}

	if err := s.dnsProxy.Start(); err != nil {
		log.Fatalf("alert: category='DNS-Proxy' %s", err.Error())
	}
	return nil
}

func (s *Transproxy) Stop() {
	s.dnsProxy.Stop()

	for _, proxy := range s.proxies {
		proxy.Stop()
	}
}
