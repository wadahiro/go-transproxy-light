package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/comail/colog"
	transproxy "github.com/wadahiro/go-transproxy-light"
)

func orPanic(err error) {
	if err != nil {
		panic(err)
	}
}

var (
	fs       = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	loglevel = fs.String(
		"loglevel",
		"info",
		"Log level, one of: debug, info, warn, error, fatal, panic",
	)

	privateDNS = fs.String("private-dns", "",
		"Private DNS addresses for no_proxy targets (IP[:port],IP[:port],...)")

	proxyListenPorts = fs.String(
		"proxy-listen-ports", "80,443,22", "Listen ports for transparent proxy, as `port1,port2,...`",
	)

	dnsProxyListenAddress = fs.String(
		"dns-listen", ":53", "DNS listen address, as `[host]:port`",
	)

	startLocalIP = fs.String(
		"start-ip", "127.0.1.0", "Start of local IP address, as `127.0.1.0`",
	)

	endLocalIP = fs.String(
		"end-ip", "127.0.255.255", "End of local IP address, as `127.0.255.255`",
	)

	dnsEnableTCP = fs.Bool("dns-tcp", true, "DNS Listen on TCP")
	dnsEnableUDP = fs.Bool("dns-udp", true, "DNS Listen on UDP")
)

func main() {
	fs.Usage = func() {
		_, exe := filepath.Split(os.Args[0])
		fmt.Fprint(os.Stderr, "go-transproxy-light.\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n\n  %s [options]\n\nOptions:\n\n", exe)
		fs.PrintDefaults()
	}
	fs.Parse(os.Args[1:])

	// seed the global random number generator, used in secureoperator
	rand.Seed(time.Now().UTC().UnixNano())

	// setup logger
	colog.SetDefaultLevel(colog.LDebug)
	colog.SetMinLevel(colog.LInfo)
	level, err := colog.ParseLevel(*loglevel)
	if err != nil {
		log.Fatalf("alert: Invalid log level: %s", err.Error())
	}
	colog.SetMinLevel(level)
	colog.SetFormatter(&colog.StdFormatter{
		Colors: true,
		Flag:   log.Ldate | log.Ltime | log.Lmicroseconds,
	})
	colog.ParseFields(true)
	colog.Register()

	startProxy(level)
}

func startProxy(level colog.Level) {
	// handling no_proxy environment
	noProxy := os.Getenv("no_proxy")
	if noProxy == "" {
		noProxy = os.Getenv("NO_PROXY")
	}
	np := parseNoProxy(noProxy)

	// ports for http
	ports := toPorts(*proxyListenPorts)

	proxy := transproxy.NewTransproxy(
		transproxy.TransproxyConfig{
			DNSListenAddress: *dnsProxyListenAddress,
			DNSEnableUDP:     *dnsEnableUDP,
			DNSEnableTCP:     *dnsEnableTCP,
			PrivateDNS:       *privateDNS,
			StartLocalIP:     *startLocalIP,
			EndLocalIP:       *endLocalIP,

			ProxyListenPorts: ports,

			NoProxy: np,
		},
	)
	proxy.Start()

	// serve until exit
	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	log.Printf("info: Proxy servers stopping.")

	proxy.Stop()

	log.Printf("info: go-transproxy exited.")
}

func toPort(addr string) int {
	array := strings.Split(addr, ":")
	if len(array) != 2 {
		log.Printf("alert: Invalid address, no port: %s", addr)
	}

	i, err := strconv.Atoi(array[1])
	if err != nil {
		log.Printf("alert: Invalid address, the port isn't number: %s", addr)
	}

	if i > 65535 || i < 0 {
		log.Printf("alert: Invalid address, the port must be an integer value in the range 0-65535: %s", addr)
	}

	return i
}

func toPorts(ports string) []int {
	array := strings.Split(ports, ",")

	var p []int

	for _, v := range array {
		i, err := strconv.Atoi(v)
		if err != nil {
			log.Printf("alert: Invalid port, It's not number: %s", ports)
		}

		if i > 65535 || i < 0 {
			log.Printf("alert: Invalid port, It must be an integer value in the range 0-65535: %s", ports)
		}

		p = append(p, i)
	}

	return p
}

func parseNoProxy(noProxy string) transproxy.NoProxy {
	p := strings.Split(noProxy, ",")

	var ipArray []string
	var cidrArray []*net.IPNet
	var domainArray []string

	for _, v := range p {
		ip := net.ParseIP(v)
		if ip != nil {
			ipArray = append(ipArray, v)
			continue
		}

		_, ipnet, err := net.ParseCIDR(v)
		if err == nil {
			cidrArray = append(cidrArray, ipnet)
			continue
		}

		domainArray = append(domainArray, v)
	}

	return transproxy.NoProxy{
		IPs:     ipArray,
		CIDRs:   cidrArray,
		Domains: domainArray,
	}
}
