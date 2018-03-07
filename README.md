# go-transproxy-light

Transparent proxy servers for HTTP, HTTPS, SSH etc. with some limitation.
This repository is heavily under development.

## Description

**go-transproxy-light** provides transparent proxy servers for HTTP, HTTPS, SSH etc. with single binary.
**This is not true transparent proxy** becasuse you need to change your DNS server setting.
However, it works not only on Linux but also on Windows because it doesn't use `iptables`.

## Limitation
There are some limitation compared to [go-transporxy](https://github.com/wadahiro/go-transproxy).

* Direct access with IP address cannot be proxied transparently. It supports FQDN access only!
* Your proxy server needs to support CONNECT method for any ports.
* Application which has own DNS cache might cause trouble.


## Install

### Binaly install
Download from [Releases page](https://github.com/wadahiro/go-transproxy-light/releases).

### Source install
Use Go 1.9 and [dep](https://github.com/golang/dep).

```
dep ensure
go build -o transproxy-light cmd/transproxy-light/main.go
chmod +x transproxy-light
```

## Usage

```
Usage:

  transproxy-light [options]

Options:

  -dns-listen [host]:port
        DNS listen address, as [host]:port (default ":53")
  -dns-tcp
        DNS Listen on TCP (default true)
  -dns-udp
        DNS Listen on UDP (default true)
  -end-ip 127.0.255.255
        End of local IP address, as 127.0.255.255 (default "127.0.255.255")
  -loglevel string
        Log level, one of: debug, info, warn, error, fatal, panic (default "info")
  -private-dns string
        Private DNS addresses for no_proxy targets (IP[:port],IP[:port],...)
  -proxy-listen-ports port1,port2,...
        Listen ports for transparent proxy, as port1,port2,... (default "80,443,22")
  -start-ip 127.0.1.0
        Start of local IP address, as 127.0.1.0 (default "127.0.1.0")
```

Proxy configuration is used from standard environment variables, `http_proxy` and `no_proxy`.
Also you can use **IP Address**, **CIDR**, **Suffix Domain Name** in `no_proxy`.

### Example 

```
# Set your proxy environment
export http_proxy=http://foo:bar@yourproxy.example.org:3128

# Set no_proxy if you need to access directly for internal
export no_proxy=192.168.0.0/24

# Start go-transproxy-light with admin privileges(sudo)
sudo -E transproxy-light -private-dns 192.168.0.100
```

Then, you need to change your DNS server for using DNS proxy server of the transproxy-light.
For example, change your `/etc/resolv.conf` as follows if you use Linux.

```
nameserver 127.0.0.1
``` 

Now, you can access to 80, 443 and 22 port transparently.

```
curl http://www.google.com
<HTML><HEAD><meta http-equiv="content-type" content="text/html;charset=utf-8">
<TITLE>302 Moved</TITLE></HEAD><BODY>
<H1>302 Moved</H1>
The document has moved
<A HREF="http://www.google.co.jp/?gfe_rd=cr&amp;dcr=0&amp;ei=GCKtWbD0AaLEXuTmr7gK">here</A>.
</BODY></HTML>
```

## Licence

Licensed under the [MIT](/LICENSE) license.

## Author

[Hiroyuki Wada](https://github.com/wadahiro)

