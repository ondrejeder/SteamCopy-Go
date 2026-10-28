//go:build linux

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func freeBytes(path string) (uint64, error) {
	var s syscall.Statfs_t
	if e := syscall.Statfs(path, &s); e != nil {
		return 0, e
	}
	return s.Bavail * uint64(s.Bsize), nil
}
func isFAT32(path string) (bool, error) {
	var s syscall.Statfs_t
	if e := syscall.Statfs(path, &s); e != nil {
		return false, e
	}
	return uint64(s.Type) == 0x4d44, nil
}
func roots() []string {
	h, _ := os.UserHomeDir()
	return []string{"/", h, "/run/media", "/media", "/mnt"}
}
func mountedVolumes() []string {
	b, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return nil
	}
	var out []string
	replacer := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		p := replacer.Replace(fields[4])
		if isWithin(p, "/run/media") || isWithin(p, "/media") || isWithin(p, "/mnt") {
			out = append(out, p)
		}
	}
	return out
}
func externalVolumes() []string {
	b, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return nil
	}
	var out []string
	replacer := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 5 {
			continue
		}
		device, err := filepath.EvalSymlinks(filepath.Join("/sys/dev/block", f[2]))
		if err != nil {
			continue
		}
		if strings.Contains(device, "/usb") {
			out = append(out, replacer.Replace(f[4]))
		}
	}
	return out
}
func steamRoots() []string {
	h, _ := os.UserHomeDir()
	return []string{filepath.Join(h, ".steam", "steam"), filepath.Join(h, ".local", "share", "Steam"), filepath.Join(h, ".var", "app", "com.valvesoftware.Steam", ".local", "share", "Steam")}
}
func openBrowser(url string) {
	for _, name := range []string{"chromium", "chromium-browser", "google-chrome-stable", "google-chrome", "microsoft-edge"} {
		if path, err := exec.LookPath(name); err == nil {
			if err := exec.Command(path, "--new-window", "--app="+url, "--window-size=1100,780").Start(); err == nil {
				return
			}
		}
	}
	if path, err := exec.LookPath("firefox"); err == nil {
		if err := exec.Command(path, "--new-window", url).Start(); err == nil {
			return
		}
	}
	if flatpak, err := exec.LookPath("flatpak"); err == nil {
		for _, app := range []string{"org.chromium.Chromium", "org.mozilla.firefox"} {
			if exec.Command(flatpak, "info", app).Run() != nil {
				continue
			}
			args := []string{"run", app}
			if app == "org.mozilla.firefox" {
				args = append(args, "--new-window", url)
			} else {
				args = append(args, "--new-window", "--app="+url, "--window-size=1100,780")
			}
			if exec.Command(flatpak, args...).Start() == nil {
				return
			}
		}
	}
	_ = exec.Command("xdg-open", url).Start()
}
