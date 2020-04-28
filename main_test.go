package main

import (
	"net"
	"testing"
)

func TestNextAddress(t *testing.T) {
	var tests = []struct {
		ip   net.IP
		want net.IP
	}{
		{net.ParseIP("127.0.0.1"), net.ParseIP("127.0.0.2")},
		{net.ParseIP("192.168.100.50"), net.ParseIP("192.168.100.51")},
		{net.ParseIP("255.255.255.255"), net.ParseIP("0.0.0.0")},
		{net.ParseIP("::1"), nil},     // Invalid IPv4 address
		{net.ParseIP("blablab"), nil}, // Invalid IPv4 address
	}

	for _, test := range tests {
		nextAddr := nextAddress(test.ip)
		if !nextAddr.Equal(test.want) {
			t.Errorf("invalid next IP address. got: %s, want: %s", nextAddr, test.want)
		}
	}
}
