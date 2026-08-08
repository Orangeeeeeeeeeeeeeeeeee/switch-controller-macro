package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// GetHidrawForUSB finds the hidraw path for a specific USB Bus and Device Address
func GetHidrawForUSB(targetBus int, targetAddr int) (string, error) {
	base := "/sys/class/hidraw"
	entries, err := ioutil.ReadDir(base)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", base, err)
	}

	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "hidraw") {
			continue
		}

		// Check if this hidraw node belongs to the target USB device
		// /sys/class/hidraw/hidrawX/device -> ... -> USB Device
		hidPath := filepath.Join(base, entry.Name(), "device")
		if matchesUSBDevice(hidPath, targetBus, targetAddr) {
			return "/dev/" + entry.Name(), nil
		}
	}

	return "", fmt.Errorf("no hidraw device found for USB Bus %d Device %d", targetBus, targetAddr)
}

// GetEvdevForUSB finds the /dev/input/eventX path for a specific USB Bus/Address
func GetEvdevForUSB(targetBus int, targetAddr int) (string, error) {
	base := "/sys/class/input"
	entries, err := ioutil.ReadDir(base)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", base, err)
	}

	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "event") {
			continue
		}

		// Check if this input node belongs to the target USB device
		// /sys/class/input/eventX/device -> ... -> USB Device
		devPath := filepath.Join(base, entry.Name(), "device")
		if matchesUSBDevice(devPath, targetBus, targetAddr) {
			return filepath.Join("/dev/input", entry.Name()), nil
		}
	}

	return "", fmt.Errorf("no evdev node found for USB Bus %d Device %d", targetBus, targetAddr)
}

// matchesUSBDevice walks up the sysfs tree to find if a path belongs to a specific USB Bus/Addr
func matchesUSBDevice(startPath string, targetBus, targetAddr int) bool {
	realPath, err := filepath.EvalSymlinks(startPath)
	if err != nil {
		return false
	}

	// Walk up the directory tree looking for "busnum" and "devnum" files
	// We limit the depth to avoid infinite loops, though sysfs is finite
	dir := realPath
	for i := 0; i < 6; i++ { // Check up to 6 levels up
		busFile := filepath.Join(dir, "busnum")
		devFile := filepath.Join(dir, "devnum")

		if fileExists(busFile) && fileExists(devFile) {
			bus, _ := readIntFile(busFile)
			addr, _ := readIntFile(devFile)
			if bus == targetBus && addr == targetAddr {
				return true
			}
			// Found a USB device but numbers didn't match
			return false
		}

		// Move up
		dir = filepath.Clean(filepath.Join(dir, ".."))
		if dir == "/" || dir == "." {
			break
		}
	}
	return false
}

func readIntFile(path string) (int, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}
