package transproxy

import (
	"fmt"
	"log"
	"net/url"
	"strings"
)

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
	PrivateDNS       []string
	NoProxy          []string
	StartLocalIP     string
	EndLocalIP       string

	ProxyListenPorts []int
	ProxyURL         *url.URL
}

func NewTransproxy(c TransproxyConfig) *Transproxy {
	// Add proxy host to no_proxy list
	proxyHost := strings.Split(c.ProxyURL.Host, ":")
	c.NoProxy = append(c.NoProxy, proxyHost[0])

	dnsProxy := NewDNSProxy(
		DNSProxyConfig{
			DNSListenAddress: c.DNSListenAddress,
			DNSEnableUDP:     c.DNSEnableUDP,
			DNSEnableTCP:     c.DNSEnableTCP,
			PrivateDNS:       c.PrivateDNS,
			NoProxy:          c.NoProxy,
			StartLocalIP:     c.StartLocalIP,
			EndLocalIP:       c.EndLocalIP,
		},
	)

	proxies := []Proxy{}
	for _, p := range c.ProxyListenPorts {
		proxy := NewPassThroughProxy(
			PassThroughProxyConfig{
				ListenAddress: fmt.Sprintf(":%d", p),
				ProxyURL:      c.ProxyURL,
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
