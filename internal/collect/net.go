package collect

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type NetCounters struct {
	RX     uint64
	TX     uint64
	Ifaces []string // names of the physical interfaces summed above, sorted
}

var netExclude = []string{"lo", "docker", "veth", "br-", "virbr", "vnet"}

func isVirtualIface(name string) bool {
	for _, p := range netExclude {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// ParseNetDev sums rx/tx bytes over physical interfaces and records their
// names. In /proc/net/dev the value after the interface colon has rx-bytes at
// index 0 and tx-bytes at index 8.
func ParseNetDev(r io.Reader) NetCounters {
	var c NetCounters
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		name := strings.TrimSpace(line[:colon])
		if name == "" || isVirtualIface(name) {
			continue
		}
		f := strings.Fields(line[colon+1:])
		if len(f) < 9 {
			continue
		}
		rx, _ := strconv.ParseUint(f[0], 10, 64)
		tx, _ := strconv.ParseUint(f[8], 10, 64)
		c.RX += rx
		c.TX += tx
		c.Ifaces = append(c.Ifaces, name)
	}
	// Sorted so the card subtitle does not reshuffle between ticks.
	sort.Strings(c.Ifaces)
	return c
}

func ReadNetDev(hostProc string) (NetCounters, error) {
	f, err := os.Open(filepath.Join(hostProc, "net", "dev"))
	if err != nil {
		return NetCounters{}, err
	}
	defer f.Close()
	return ParseNetDev(f), nil
}
