package transproxy

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	ps "github.com/gorillalabs/go-powershell"
	"github.com/gorillalabs/go-powershell/backend"
)

type DNSServerAddress struct {
	InterfaceIndex  int      `json:"InterfaceIndex"`
	InterfaceAlias  string   `json:"InterfaceAlias"`
	ServerAddresses []string `json:"ServerAddresses"`
}

type NetIPInterface struct {
	InterfaceIndex int    `json:"InterfaceIndex"`
	InterfaceAlias string `json:"InterfaceAlias"`
	Dhcp           int    `json:"Dhcp"`
}

type DNSSetting struct {
	InterfaceIndex  int
	InterfaceAlias  string
	Dhcp            bool
	ServerAddresses []string
}

func (s *DNSProxy) Setup() {
	currentSettings := []DNSSetting{}

	// start a local powershell process
	back := &backend.Local{}
	shell, err := ps.New(back)
	if err != nil {
		log.Printf("warn: category='DNS-Proxy[windows]' DNS Setup failed: %s", err.Error())
		return
	}
	defer shell.Exit()

	stdout, _, err := shell.Execute("Get-DnsClientServerAddress -AddressFamily IPv4 | ConvertTo-Json")
	if err != nil {
		log.Printf("warn: category='DNS-Proxy[windows]' DNS Setup failed: %s", err.Error())
		return
	}
	j := ([]byte)(stdout)
	dnsServerAddresses := []DNSServerAddress{}
	if err := json.Unmarshal(j, &dnsServerAddresses); err != nil {
		log.Printf("warn: category='DNS-Proxy[windows]' DNS Setup failed: %s", err.Error())
		return
	}

	stdout, _, err = shell.Execute("Get-NetIPInterface -AddressFamily IPv4 | ConvertTo-Json")
	if err != nil {
		log.Printf("warn: category='DNS-Proxy[windows]' DNS Setup failed: %s", err.Error())
		return
	}
	j = ([]byte)(stdout)
	netIPInterfaces := []NetIPInterface{}
	if err := json.Unmarshal(j, &netIPInterfaces); err != nil {
		log.Printf("warn: category='DNS-Proxy[windows]' DNS Setup failed: %s", err.Error())
		return
	}

	for _, nii := range netIPInterfaces {
		for _, dsa := range dnsServerAddresses {
			if nii.InterfaceIndex == dsa.InterfaceIndex {
				dhcp := false
				if nii.Dhcp == 1 {
					dhcp = true
				}
				if len(dsa.ServerAddresses) > 0 {
					currentSettings = append(currentSettings, DNSSetting{
						InterfaceIndex:  nii.InterfaceIndex,
						InterfaceAlias:  nii.InterfaceAlias,
						Dhcp:            dhcp,
						ServerAddresses: dsa.ServerAddresses,
					})
				}
			}
		}
	}

	// Save curret settings into the memory for teardown
	s.dnsSettings = currentSettings

	// Change DNS!
	for _, setting := range currentSettings {
		stdout, _, err = shell.Execute(fmt.Sprintf("Set-DnsClientServerAddress -InterfaceIndex %d -ServerAddresses (\"%s\")", setting.InterfaceIndex, "127.0.0.1"))
		if err != nil {
			log.Printf("warn: category='DNS-Proxy[windows]' DNS Setup failed: %s", err.Error())
		}
	}
}

func (s *DNSProxy) Teardown() {
	settings, ok := s.dnsSettings.([]DNSSetting)
	if !ok {
		log.Printf("warn: category='DNS-Proxy[windows]' DNS Teardown failed: %v", settings)
		return
	}

	// start a local powershell process
	back := &backend.Local{}
	shell, err := ps.New(back)
	if err != nil {
		log.Printf("warn: category='DNS-Proxy[windows]' DNS Teardown failed: %s", err.Error())
		return
	}

	defer shell.Exit()
	for _, setting := range settings {
		if setting.Dhcp {
			_, _, err := shell.Execute(fmt.Sprintf("Set-DnsClientServerAddress -InterfaceIndex %d -ResetServerAddresses", setting.InterfaceIndex))
			if err != nil {
				log.Printf("warn: category='DNS-Proxy[windows]' DNS Teardown failed: %s", err.Error())
			}

		} else {
			servers := strings.Join(setting.ServerAddresses, "\",\"")
			_, _, err := shell.Execute(fmt.Sprintf("Set-DnsClientServerAddress -InterfaceIndex %d -ServerAddresses (\"%s\")", setting.InterfaceIndex, servers))
			if err != nil {
				log.Printf("warn: category='DNS-Proxy[windows]' DNS Teardown failed: %s", err.Error())
			}
		}
	}
}
