// Package netinfo 找出手機可以用來連這台電腦的 IPv4 位址。
package netinfo

import (
	"net"
	"sort"
	"strings"
)

// Addr 一個位址。Kind：lan（家裡的區網）、vpn（Tailscale 這類，手機也要裝才連得到）
type Addr struct {
	IP    string `json:"ip"`
	Iface string `json:"iface"`
	Kind  string `json:"kind"`
}

// 虛擬網卡（Docker、WSL、Hyper-V、VirtualBox…）的位址手機連不到，不列出來
var virtualPrefixes = []string{"docker", "br-", "veth", "virbr", "vethernet", "vmnet", "vboxnet", "lxc", "lxd", "cni", "flannel", "podman"}

func isVirtual(iface string) bool {
	name := strings.ToLower(iface)
	for _, p := range virtualPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

var cgnat = net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// Classify 判斷一個介面上的位址能不能給手機用；回傳種類，不能用回傳空字串
func Classify(ip net.IP, iface string) string {
	ip4 := ip.To4()
	if ip4 == nil || ip4.IsLoopback() || ip4.IsLinkLocalUnicast() || ip4.IsUnspecified() || isVirtual(iface) {
		return ""
	}
	if cgnat.Contains(ip4) || strings.Contains(strings.ToLower(iface), "tailscale") {
		return "vpn"
	}
	if ip4.IsPrivate() {
		return "lan"
	}
	return ""
}

// LAN 目前可以用的位址，區網的排前面
func LAN() []Addr {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []Addr
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			if kind := Classify(ipn.IP, ifc.Name); kind != "" {
				out = append(out, Addr{IP: ipn.IP.To4().String(), Iface: ifc.Name, Kind: kind})
			}
		}
	}
	Sort(out)
	return out
}

// Sort 區網在前、VPN 在後，同類照位址排
func Sort(addrs []Addr) {
	rank := map[string]int{"lan": 0, "vpn": 1}
	sort.SliceStable(addrs, func(i, j int) bool {
		if rank[addrs[i].Kind] != rank[addrs[j].Kind] {
			return rank[addrs[i].Kind] < rank[addrs[j].Kind]
		}
		return addrs[i].IP < addrs[j].IP
	})
}
