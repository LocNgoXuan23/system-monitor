package collect

import (
	"strings"
	"testing"
)

func TestParseMounts(t *testing.T) {
	in := "sysfs /sys sysfs rw 0 0\n" +
		"/dev/sda1 / ext4 rw 0 0\n" +
		"tmpfs /run tmpfs rw 0 0\n" +
		"/dev/nvme0n1p2 /data xfs rw 0 0\n" +
		"/dev/sda1 / ext4 rw 0 0\n" // duplicate device
	ms := ParseMounts(strings.NewReader(in))
	if len(ms) != 2 {
		t.Fatalf("len=%d want 2, got %+v", len(ms), ms)
	}
	if ms[0].Mountpoint != "/" || ms[0].FSType != "ext4" {
		t.Errorf("ms[0]=%+v", ms[0])
	}
	if ms[1].Mountpoint != "/data" {
		t.Errorf("ms[1]=%+v", ms[1])
	}
}
