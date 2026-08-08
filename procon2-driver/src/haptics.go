package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"time"
)

// HapticPattern represents a haptic vibration pattern
type HapticPattern [][]byte

// DefaultHapticPattern is a simple test pattern
var DefaultHapticPattern = HapticPattern{
	{0x93, 0x35, 0x36, 0x1c, 0x0d},
	{0xa8, 0x29, 0xc5, 0xdc, 0x0c},
	{0x75, 0x21, 0xb5, 0x5d, 0x13},
}

// HapticPlayer handles haptic feedback
type HapticPlayer struct {
	file   *os.File
	report [64]byte
}

// NewHapticPlayer opens a HID device for haptic output
func NewHapticPlayer(hidPath string) (*HapticPlayer, error) {
	f, err := os.OpenFile(hidPath, os.O_RDWR|os.O_SYNC, 0)
	if err != nil {
		return nil, fmt.Errorf("open hidraw: %w (try running as root or add udev rule)", err)
	}

	return &HapticPlayer{file: f, report: [64]byte{}}, nil
}

// Close closes the haptic device
func (h *HapticPlayer) Close() error {
	if h.file != nil {
		return h.file.Close()
	}
	return nil
}

// Play plays a haptic pattern with the specified frame interval and timeout
func (h *HapticPlayer) Play(pattern HapticPattern, frameInterval time.Duration, timeout time.Duration) error {
	ticker := time.NewTicker(frameInterval)
	defer ticker.Stop()

	done := make(chan error, 1)

	go func() {
		counter := byte(0)

		for i, frame := range pattern {
			<-ticker.C

			for j := range h.report {
				h.report[j] = 0
			}

			h.report[0] = 0x02
			h.report[1] = 0x50 | (counter & 0x0F)
			h.report[17] = h.report[1]

			// Copy frame data into the pre-allocated slots
			copy(h.report[2:7], frame)
			copy(h.report[18:23], frame)

			n, err := h.file.Write(h.report[:])
			if err != nil {
				done <- fmt.Errorf("write error at frame %d: %w", i, err)
				return
			}
			if n != len(h.report) {
				done <- fmt.Errorf("short write at frame %d: %d/%d bytes", i, n, len(h.report))
				return
			}

			log.Printf("Sent haptic frame %d/%d (counter 0x%02x)", i+1, len(pattern), counter)
			counter = (counter + 1) & 0x0F
		}

		// Send stop report
		<-ticker.C
		stop := make([]byte, 64)
		stop[0] = 0x02
		stop[1] = 0x50
		stop[17] = stop[1]

		if _, err := h.file.Write(stop); err != nil {
			done <- fmt.Errorf("error sending stop report: %w", err)
		} else {
			log.Println("Sent haptic stop report")
		}

		done <- nil
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		return errors.New("haptics timed out")
	}
}

// PlaySimple plays the default haptic pattern
func (h *HapticPlayer) PlaySimple() error {
	return h.Play(DefaultHapticPattern, 4*time.Millisecond, 5*time.Second)
}
