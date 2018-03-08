package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"comail.io/go/wincolog"
	"github.com/BurntSushi/toml"
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
	logLevel = fs.String(
		"loglevel",
		"info",
		"Log level, one of: debug, info, warn, error, fatal, panic",
	)

	dns = fs.String("dns", "",
		"DNS servers for no_proxy targets (IP[:port],IP[:port],...)")

	port = fs.String(
		"port", "80,443,22", "Listen ports for transparent proxy, as `port1,port2,...`",
	)

	loopbackAddressRange = fs.String(
		"loopback-address-range", "127.0.1.0-127.0.255.255", "Range of local IP address, as `127.0.1.0-127.0.255.255`",
	)
)

type Config struct {
	ProxyURL             string
	NoProxy              []string
	DNS                  []string
	Port                 []int
	LogLevel             string
	LoopbackAddressRange string
}

func main() {
	// Configure from cli options or config.toml
	var config Config
	fs.Usage = func() {
		_, exe := filepath.Split(os.Args[0])
		fmt.Fprint(os.Stderr, "go-transproxy-light.\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n\n  %s [options]\n\nOptions:\n\n", exe)
		fs.PrintDefaults()
	}

	_, err := toml.DecodeFile("config.toml", &config)
	if err != nil {
		fs.Parse(os.Args[1:])

		proxyUrl := os.Getenv("http_proxy")
		noProxy := strings.Split(os.Getenv("no_proxy"), ",")
		dnsServers := strings.Split(*dns, ",")
		listenPort := toPorts(*port)

		config = Config{
			ProxyURL:             proxyUrl,
			NoProxy:              noProxy,
			DNS:                  dnsServers,
			Port:                 listenPort,
			LogLevel:             *logLevel,
			LoopbackAddressRange: *loopbackAddressRange,
		}
	}

	// seed the global random number generator, used in secureoperator
	rand.Seed(time.Now().UTC().UnixNano())

	// setup logger
	colog.SetDefaultLevel(colog.LDebug)
	colog.SetFormatter(&colog.StdFormatter{
		Colors: true,
		Flag:   log.Ldate | log.Ltime | log.Lmicroseconds,
	})
	if runtime.GOOS == "windows" {
		colog.SetOutput(wincolog.Stdout())
	}
	colog.ParseFields(true)
	colog.Register()

	level, err := colog.ParseLevel(config.LogLevel)
	if err != nil {
		log.Fatalf("alert: Invalid log level: %s", err)
	}
	colog.SetMinLevel(level)

	startProxy(config)
}

func startProxy(config Config) {
	loopback := parseLoopBackAddressRange(config.LoopbackAddressRange)
	proxyURL := parseProxyURL(config.ProxyURL)

	proxy := transproxy.NewTransproxy(
		transproxy.TransproxyConfig{
			DNSListenAddress: ":53",
			DNSEnableUDP:     true,
			DNSEnableTCP:     true,
			PrivateDNS:       config.DNS,
			StartLocalIP:     loopback[0],
			EndLocalIP:       loopback[1],

			ProxyListenPorts: config.Port,
			ProxyURL:         proxyURL,
			NoProxy:          config.NoProxy,
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

func parseLoopBackAddressRange(s string) []string {
	defaultRange := []string{"127.0.1.0", "127.0.255.255"}

	if s == "" {
		log.Printf("info: Use default range %s-%s", defaultRange[0], defaultRange[1])
		return defaultRange
	}
	loopback := strings.Split(s, "-")
	if len(loopback) != 2 {
		log.Fatalf("alert: Invalid loopback address range: %s", s)
	}

	startIP := net.ParseIP(loopback[0])
	endIP := net.ParseIP(loopback[1])

	if startIP == nil || endIP == nil {
		log.Fatalf("alert: Invalid loopback address range (Invalid IP format): %s", s)
	}

	start := ip2int(startIP)
	end := ip2int(endIP)
	if !strings.HasPrefix(loopback[0], "127.") || !strings.HasPrefix(loopback[1], "127.") ||
		loopback[0] == "127.0.0.0" || loopback[1] == "127.255.255.255" ||
		start >= end {
		log.Fatalf("alert: Invalid loopback address range (Need to set from 127.0.0.1 to 127.255.255.254): %s", s)
	}

	return loopback
}

func ip2int(ip net.IP) uint32 {
	if len(ip) == 16 {
		return binary.BigEndian.Uint32(ip[12:16])
	}
	return binary.BigEndian.Uint32(ip)
}

func parseProxyURL(proxyURL string) *url.URL {
	if proxyURL == "" {
		log.Fatalf("alert: Not configured http_proxy")
	}

	u, err := url.Parse(proxyURL)
	if err != nil {
		log.Fatalf("alert: Invalid http_proxy: %s", err)
	}
	return u
}
