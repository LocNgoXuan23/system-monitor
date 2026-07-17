package collect

import (
	"strings"
	"testing"
)

const netDevSample = "Inter-|   Receive                    |  Transmit\n" +
	" face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets\n" +
	"    lo:  1000    10 0 0 0 0 0 0  1000 10 0 0 0 0 0 0\n" +
	"  eth0:  5000    50 0 0 0 0 0 0  2000 20 0 0 0 0 0 0\n" +
	" veth1:  9999    99 0 0 0 0 0 0  9999 99 0 0 0 0 0 0\n"

func TestParseNetDev(t *testing.T) {
	c := ParseNetDev(strings.NewReader(netDevSample))
	if c.RX != 5000 || c.TX != 2000 { // only eth0 counted
		t.Errorf("got RX=%d TX=%d, want 5000/2000", c.RX, c.TX)
	}
}

func TestParseNetDevIfaces(t *testing.T) {
	c := ParseNetDev(strings.NewReader(netDevSample))
	if len(c.Ifaces) != 1 || c.Ifaces[0] != "eth0" {
		t.Errorf("got %v, want [eth0] — lo and veth1 must be excluded", c.Ifaces)
	}
}

func TestParseNetDevIfacesSorted(t *testing.T) {
	in := " wlan0:  10 1 0 0 0 0 0 0  10 1 0 0 0 0 0 0\n" +
		"enp5s0:  20 2 0 0 0 0 0 0  20 2 0 0 0 0 0 0\n"
	c := ParseNetDev(strings.NewReader(in))
	if len(c.Ifaces) != 2 || c.Ifaces[0] != "enp5s0" || c.Ifaces[1] != "wlan0" {
		t.Errorf("got %v, want [enp5s0 wlan0] in sorted order", c.Ifaces)
	}
}

func TestParseNetDevNoPhysical(t *testing.T) {
	c := ParseNetDev(strings.NewReader("    lo:  1 1 0 0 0 0 0 0  1 1 0 0 0 0 0 0\n"))
	if len(c.Ifaces) != 0 {
		t.Errorf("got %v, want none", c.Ifaces)
	}
}
