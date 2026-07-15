package collect

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"system-monitor/internal/model"
)

type Mount struct {
	Device     string
	Mountpoint string
	FSType     string
}

var realFS = map[string]bool{
	"ext2": true, "ext3": true, "ext4": true, "xfs": true, "btrfs": true,
	"vfat": true, "exfat": true, "ntfs": true, "ntfs3": true, "f2fs": true, "zfs": true,
}

func ParseMounts(r io.Reader) []Mount {
	var out []Mount
	seen := map[string]bool{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 3 {
			continue
		}
		dev, mp, fstype := f[0], f[1], f[2]
		if !realFS[fstype] || seen[dev] {
			continue
		}
		seen[dev] = true
		out = append(out, Mount{Device: dev, Mountpoint: mp, FSType: fstype})
	}
	return out
}

// ReadFS statfs's each real mount. It reads PID 1's mount table (the host init's
// mount namespace) rather than self/mounts: inside the container the self symlink
// resolves to the container's own mount table (mountpoints already prefixed with
// /host/root), which would double-prefix under hostRoot. Host mountpoints are then
// resolved under hostRoot (the host filesystem bind-mounted read-only into the container).
func ReadFS(hostProc, hostRoot string) []model.FSInfo {
	f, err := os.Open(filepath.Join(hostProc, "1", "mounts"))
	if err != nil {
		return nil
	}
	defer f.Close()
	mounts := ParseMounts(f)
	var out []model.FSInfo
	for _, m := range mounts {
		p := filepath.Join(hostRoot, m.Mountpoint)
		var st syscall.Statfs_t
		if err := syscall.Statfs(p, &st); err != nil {
			continue
		}
		bs := uint64(st.Bsize)
		total := st.Blocks * bs
		if total == 0 {
			continue
		}
		used := total - st.Bfree*bs
		out = append(out, model.FSInfo{
			Mount: m.Mountpoint,
			Used:  used,
			Total: total,
			Pct:   float64(used) / float64(total) * 100,
		})
	}
	return out
}
