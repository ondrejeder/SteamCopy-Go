//go:build windows

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var kernel32 = syscall.NewLazyDLL("kernel32.dll")
var getFree = kernel32.NewProc("GetDiskFreeSpaceExW")
var getDrives = kernel32.NewProc("GetLogicalDrives")
var getDriveType = kernel32.NewProc("GetDriveTypeW")
var getVolumePath = kernel32.NewProc("GetVolumePathNameW")
var getVolumeInfo = kernel32.NewProc("GetVolumeInformationW")

func isFAT32(path string) (bool, error) {
	p, e := syscall.UTF16PtrFromString(path)
	if e != nil {
		return false, e
	}
	root := make([]uint16, 1024)
	r, _, err := getVolumePath.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&root[0])), uintptr(len(root)))
	if r == 0 {
		return false, err
	}
	fsname := make([]uint16, 64)
	r, _, err = getVolumeInfo.Call(uintptr(unsafe.Pointer(&root[0])), 0, 0, 0, 0, 0, uintptr(unsafe.Pointer(&fsname[0])), uintptr(len(fsname)))
	if r == 0 {
		return false, err
	}
	return strings.EqualFold(syscall.UTF16ToString(fsname), "FAT32"), nil
}

func freeBytes(path string) (uint64, error) {
	p, e := syscall.UTF16PtrFromString(path)
	if e != nil {
		return 0, e
	}
	var avail, total, free uint64
	r, _, err := getFree.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&avail)), uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&free)))
	if r == 0 {
		return 0, err
	}
	return avail, nil
}
func roots() []string {
	m, _, _ := getDrives.Call()
	var out []string
	for i := 0; i < 26; i++ {
		if m&(1<<i) != 0 {
			out = append(out, fmt.Sprintf("%c:\\", 'A'+i))
		}
	}
	return out
}
func mountedVolumes() []string {
	var out []string
	for _, root := range roots() {
		p, e := syscall.UTF16PtrFromString(root)
		if e != nil {
			continue
		}
		t, _, _ := getDriveType.Call(uintptr(unsafe.Pointer(p)))
		if t == 2 || t == 3 {
			out = append(out, root)
		}
	}
	return out
}
func externalVolumes() []string {
	seen := map[string]bool{}
	for _, root := range roots() {
		p, e := syscall.UTF16PtrFromString(root)
		if e != nil {
			continue
		}
		t, _, _ := getDriveType.Call(uintptr(unsafe.Pointer(p)))
		if t == 2 {
			seen[strings.ToUpper(root)] = true
		}
	}
	// USB disks are frequently reported as fixed drives, so inspect their bus.
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	const script = `$u=@(Get-Disk -ErrorAction Stop | Where-Object { $_.BusType -in @('USB','SD','MMC') } | ForEach-Object { $_.Number }); Get-Partition -ErrorAction Stop | Where-Object { $_.DriveLetter -and $_.DiskNumber -in $u } | ForEach-Object { [string]$_.DriveLetter + ':\' }`
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if b, e := cmd.Output(); e == nil {
		for _, line := range strings.Split(string(b), "\n") {
			root := strings.ToUpper(strings.TrimSpace(line))
			if len(root) == 3 && root[1:] == `:\` && root[0] >= 'A' && root[0] <= 'Z' {
				seen[root] = true
			}
		}
	}
	var out []string
	for root := range seen {
		out = append(out, root)
	}
	return out
}
func steamRoots() []string {
	var out []string
	b, _ := exec.Command("reg", "query", `HKCU\Software\Valve\Steam`, "/v", "SteamPath").Output()
	for _, s := range strings.Split(string(b), "\n") {
		if i := strings.Index(s, "REG_SZ"); i >= 0 {
			out = append(out, strings.TrimSpace(s[i+len("REG_SZ"):]))
		}
	}
	out = append(out, filepath.Join("C:\\", "Program Files (x86)", "Steam"))
	return out
}
func openBrowser(url string) {
	for _, p := range []string{
		filepath.Join(os.Getenv("PROGRAMFILES(X86)"), "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(os.Getenv("PROGRAMFILES"), "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Google", "Chrome", "Application", "chrome.exe"),
	} {
		if _, e := os.Stat(p); e == nil {
			if e = exec.Command(p, "--new-window", "--app="+url, "--window-size=1100,780").Start(); e == nil {
				return
			}
		}
	}
	_ = exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", url).Start()
}
