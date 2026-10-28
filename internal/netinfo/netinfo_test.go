package netinfo

import (
	"net"
	"reflect"
	"testing"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		ip, iface, want string
	}{
		{"192.168.1.20", "Wi-Fi", "lan"},
		{"10.0.0.5", "eth0", "lan"},
		{"172.16.1.3", "enp3s0", "lan"},
		{"100.100.50.123", "Tailscale", "vpn"},
		{"100.100.45.31", "tailscale0", "vpn"},
		{"172.17.0.1", "docker0", ""},
		{"172.18.0.1", "br-1a2b", ""},
		{"172.31.255.1", "vEthernet (WSL)", ""},
		{"169.254.10.1", "Ethernet 2", ""},
		{"127.0.0.1", "lo", ""},
		{"8.8.8.8", "eth0", ""},
		{"fe80::1", "eth0", ""},
	}
	for _, c := range cases {
		if got := Classify(net.ParseIP(c.ip), c.iface); got != c.want {
			t.Errorf("Classify(%s, %s) = %q，應該是 %q", c.ip, c.iface, got, c.want)
		}
	}
}

func TestSortLANFirst(t *testing.T) {
	addrs := []Addr{{IP: "100.100.50.123", Kind: "vpn"}, {IP: "192.168.1.20", Kind: "lan"}, {IP: "10.0.0.5", Kind: "lan"}}
	Sort(addrs)
	got := []string{addrs[0].IP, addrs[1].IP, addrs[2].IP}
	if want := []string{"10.0.0.5", "192.168.1.20", "100.100.50.123"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("排序 %v，應該是 %v", got, want)
	}
}
