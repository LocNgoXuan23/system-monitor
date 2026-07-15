package collect

import (
	"strings"
	"testing"
)

func TestParseNetDev(t *testing.T) {
	in := "Inter-|   Receive                    |  Transmit\n" +
		" face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets\n" +
		"    lo:  1000    10 0 0 0 0 0 0  1000 10 0 0 0 0 0 0\n" +
		"  eth0:  5000    50 0 0 0 0 0 0  2000 20 0 0 0 0 0 0\n" +
		" veth1:  9999    99 0 0 0 0 0 0  9999 99 0 0 0 0 0 0\n"
	c := ParseNetDev(strings.NewReader(in))
	if c.RX != 5000 || c.TX != 2000 { // only eth0 counted
		t.Errorf("got RX=%d TX=%d, want 5000/2000", c.RX, c.TX)
	}
}
