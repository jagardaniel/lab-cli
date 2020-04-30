package main

import (
	"net"
	"os"
	"os/user"
	"path"
	"testing"
)

// This test also tests getConfigDir
func TestGetTemplateDir(t *testing.T) {
	// Test without XDG_CONFIG_DIR set
	os.Unsetenv("XDG_CONFIG_HOME")

	user, _ := user.Current()
	expectedDir := path.Join(user.HomeDir, ".config", "lab-cli", "templates")
	templateDir, _ := getTemplateDir()

	if templateDir != expectedDir {
		t.Errorf("did not get the template directoy we wanted. got: %s, want: %s", templateDir, expectedDir)
	}

	// With XDG_CONFIG_DIR set
	os.Setenv("XDG_CONFIG_HOME", path.Join("/tmp", ".config"))

	expectedDir = path.Join("/tmp", ".config", "lab-cli", "templates")
	templateDir, _ = getTemplateDir()

	if templateDir != expectedDir {
		t.Errorf("did not get the template directoy we wanted. got: %s, want: %s", templateDir, expectedDir)
	}
}

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
