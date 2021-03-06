package proc

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

type ProcInfo struct {
	Uid     int
	Pid     int
	loaded  bool
	ExePath string
	CmdLine string
}

type pidCache struct {
	cacheMap map[uint64]*ProcInfo
	lock     sync.Mutex
}

func (pc *pidCache) lookup(inode uint64) *ProcInfo {
	pc.lock.Lock()
	defer pc.lock.Unlock()
	pi, ok := pc.cacheMap[inode]
	if ok && pi.loadProcessInfo() {
		return pi
	}
	pc.cacheMap = loadCache()
	pi, ok = pc.cacheMap[inode]
	if ok && pi.loadProcessInfo() {
		return pi
	}
	return nil
}

func loadCache() map[uint64]*ProcInfo {
	cmap := make(map[uint64]*ProcInfo)
	for _, n := range readdir("/proc") {
		pid := toPid(n)
		if pid != 0 {
			pinfo := &ProcInfo{Pid: pid}
			for _, inode := range inodesFromPid(pid) {
				cmap[inode] = pinfo
			}
		}
	}
	return cmap
}

func toPid(name string) int {
	pid, err := strconv.ParseUint(name, 10, 32)
	if err != nil {
		return 0
	}
	fdpath := fmt.Sprintf("/proc/%d/fd", pid)
	fi, err := os.Stat(fdpath)
	if err != nil {
		return 0
	}
	if !fi.IsDir() {
		return 0
	}
	return (int)(pid)
}

func inodesFromPid(pid int) []uint64 {
	var inodes []uint64
	fdpath := fmt.Sprintf("/proc/%d/fd", pid)
	for _, n := range readdir(fdpath) {
		if link, err := os.Readlink(path.Join(fdpath, n)); err != nil {
			if !os.IsNotExist(err) {
				log.Warning("Error reading link %s: %v", n, err)
			}
		} else {
			if inode := extractSocket(link); inode > 0 {
				inodes = append(inodes, inode)
			}
		}
	}
	return inodes
}

func extractSocket(name string) uint64 {
	if !strings.HasPrefix(name, "socket:[") || !strings.HasSuffix(name, "]") {
		return 0
	}
	val := name[8 : len(name)-1]
	inode, err := strconv.ParseUint(val, 10, 64)
	if err != nil {
		log.Warning("Error parsing inode value from %s: %v", name, err)
		return 0
	}
	return inode
}

func readdir(dir string) []string {
	d, err := os.Open(dir)
	if err != nil {
		log.Warning("Error opening directory %s: %v", dir, err)
		return nil
	}
	defer d.Close()
	names, err := d.Readdirnames(0)
	if err != nil {
		log.Warning("Error reading directory names from %s: %v", dir, err)
		return nil
	}
	return names
}

func (pi *ProcInfo) loadProcessInfo() bool {
	if pi.loaded {
		return true
	}

	exePath, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pi.Pid))
	if err != nil {
		log.Warning("Error reading exe link for pid %d: %v", pi.Pid, err)
		return false
	}
	bs, err := ioutil.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pi.Pid))
	if err != nil {
		log.Warning("Error reading cmdline for pid %d: %v", pi.Pid, err)
		return false
	}
	for i, b := range bs {
		if b == 0 {
			bs[i] = byte(' ')
		}
	}

	finfo, err := os.Stat(fmt.Sprintf("/proc/%d", pi.Pid))
	if err != nil {
		log.Warning("Could not stat /proc/%d: %v", pi.Pid, err)
		return false
	}
	sys := finfo.Sys().(*syscall.Stat_t)
	pi.Uid = int(sys.Uid)
	pi.ExePath = exePath
	pi.CmdLine = string(bs)
	pi.loaded = true
	return true
}
