package main

import (
	"errors"
	"fmt"
	"math"
	"os"
	"time"
)

// Calibration constants for joystick normalization
type JoystickCalibration struct {
	LXCenter, LXMin, LXMax int
	LYCenter, LYMin, LYMax int
	RXCenter, RXMin, RXMax int
	RYCenter, RYMin, RYMax int
	Deadzone               int
}

// DefaultCalibration provides standard calibration values
var DefaultCalibration = JoystickCalibration{
	LXCenter: 2063, LXMin: 294, LXMax: 3735,
	LYCenter: 2161, LYMin: 512, LYMax: 3733,
	RXCenter: 2142, RXMin: 407, RXMax: 3628,
	RYCenter: 2050, RYMin: 368, RYMax: 3854,
	Deadzone: 50,
}

// JoystickValues holds normalized joystick positions (-1.0 to 1.0)
type JoystickValues struct {
	LX, LY float64 // Left stick
	RX, RY float64 // Right stick
}

// ControllerState represents the complete controller input state
type ControllerState struct {
	// Face buttons
	A, B, X, Y bool

	// Shoulder buttons
	L, R, ZL, ZR bool

	// D-pad
	DpadUp, DpadDown, DpadLeft, DpadRight bool

	// System buttons
	Plus, Minus, Home, Capture bool

	// Stick presses
	LStickPress, RStickPress bool

	// Paddle buttons (if available)
	PaddleLeft, PaddleRight bool

	// Joystick positions
	Joysticks JoystickValues
}

// HIDReader handles reading from a HID device
type HIDReader struct {
	file        *os.File
	calibration JoystickCalibration
	buffer      [64]byte
	stateChan   chan ControllerState
	errChan     chan error
	stopChan    chan struct{}
	debugData   []byte
	debugStats  []ByteStats
}

// NewHIDReader opens a HID device for reading
func NewHIDReader(hidPath string, cal JoystickCalibration) (*HIDReader, error) {
	f, err := os.OpenFile(hidPath, os.O_RDWR|os.O_SYNC, 0)
	if err != nil {
		return nil, fmt.Errorf("open hidraw: %w", err)
	}

	reader := &HIDReader{
		file:        f,
		calibration: cal,
		stateChan:   make(chan ControllerState, 1),
		errChan:     make(chan error, 1),
		stopChan:    make(chan struct{}),
		debugData:   make([]byte, 200*64),
		debugStats:  make([]ByteStats, 64),
	}

	// Send initialization commands
	if err := reader.sendInitCommands(); err != nil {
		f.Close()
		return nil, fmt.Errorf("init commands failed: %w", err)
	}

	go reader.runReadLoop()

	return reader, nil
}

// runReadLoop is the ONLY goroutine that reads from the file
func (r *HIDReader) runReadLoop() {
	for {
		select {
		case <-r.stopChan:
			return
		default:
			n, err := r.file.Read(r.buffer[:])
			if err != nil {
				r.errChan <- err
				return
			}
			if n >= 6 {
				state := r.parseReport(r.buffer[:n])
				// Non-blocking send: always keep the stateChan updated with the LATEST report
				select {
				case r.stateChan <- state:
				default:
					<-r.stateChan // Drain old state
					r.stateChan <- state
				}
			}
		}
	}
}

// Close closes the HID device
func (r *HIDReader) Close() error {
	close(r.stopChan)
	if r.file != nil {
		return r.file.Close()
	}
	return nil
}

// ReadState now just looks at the channel (no goroutine spawning!)
func (r *HIDReader) ReadState() (ControllerState, error) {
	select {
	case err := <-r.errChan:
		return ControllerState{}, err
	case state := <-r.stateChan:
		return state, nil
	}
}

// ReadStateTimeout is now extremely cheap to call
func (r *HIDReader) ReadStateTimeout(timeout time.Duration) (ControllerState, error) {
	select {
	case err := <-r.errChan:
		return ControllerState{}, err
	case state := <-r.stateChan:
		return state, nil
	case <-time.After(timeout):
		return ControllerState{}, errors.New("read timeout")
	}
}

// DebugReport captures and analyzes HID reports
func (r *HIDReader) DebugReport(numReports int) (*HIDDebugInfo, error) {
	requiredSize := numReports * 64
	if len(r.debugData) < requiredSize {
		r.debugData = make([]byte, requiredSize)
	}

	// Reset stats for a fresh run
	for i := range r.debugStats {
		r.debugStats[i] = ByteStats{}
	}

	reports := make([][]byte, numReports)

	for i := 0; i < numReports; i++ {
		n, err := r.file.Read(r.buffer[:])
		if err != nil {
			return nil, fmt.Errorf("read error at report %d: %w", i, err)
		}

		// Use a slice of the internal debugData block
		start := i * 64
		report := r.debugData[start : start+n]
		copy(report, r.buffer[:n])
		reports[i] = report

		// Update stats using the internal debugStats slice
		for j := 0; j < n && j < 64; j++ {
			s := &r.debugStats[j]
			val := report[j]
			if !s.Seen {
				s.Min, s.Max, s.Seen = val, val, true
			} else {
				if val < s.Min {
					s.Min = val
				}
				if val > s.Max {
					s.Max = val
				}
			}
			if i > 0 && val != reports[0][j] {
				s.Changes++
			}
		}
	}

	return &HIDDebugInfo{
		Reports: reports,
		Stats:   r.debugStats,
	}, nil
}

// HIDDebugInfo contains debug statistics
type HIDDebugInfo struct {
	Reports [][]byte
	Stats   []ByteStats
}

// ByteStats tracks statistics for a byte position
type ByteStats struct {
	Seen    bool
	Min     byte
	Max     byte
	Changes int
}

// Internal methods

func (r *HIDReader) sendInitCommands() error {
	packetNum := byte(0)

	// Set input mode to 0x30 (full controller state)
	msgMode := []byte{0x01, packetNum, 0x00, 0x01, 0x40, 0x40, 0x00, 0x01, 0x40, 0x40, 0x03, 0x30}
	if _, err := r.file.Write(msgMode); err != nil {
		return err
	}
	packetNum++
	time.Sleep(100 * time.Millisecond)

	// Set frequency
	msgFreq := []byte{0x01, packetNum, 0x00, 0x01, 0x40, 0x40, 0x00, 0x01, 0x40, 0x40, 0x03, 0x31}
	if _, err := r.file.Write(msgFreq); err != nil {
		return err
	}
	time.Sleep(100 * time.Millisecond)

	return nil
}

func (r *HIDReader) parseReport(rep []byte) ControllerState {
	state := ControllerState{}

	// Parse buttons
	if len(rep) > 3 {
		b3 := rep[3]
		state.B = (b3 & 0x01) != 0
		state.A = (b3 & 0x02) != 0
		state.Y = (b3 & 0x04) != 0
		state.X = (b3 & 0x08) != 0
		state.R = (b3 & 0x10) != 0
		state.ZR = (b3 & 0x20) != 0
		state.Plus = (b3 & 0x40) != 0
		state.RStickPress = (b3 & 0x80) != 0
	}

	if len(rep) > 4 {
		b4 := rep[4]
		state.DpadDown = (b4 & 0x01) != 0
		state.DpadRight = (b4 & 0x02) != 0
		state.DpadLeft = (b4 & 0x04) != 0
		state.DpadUp = (b4 & 0x08) != 0
		state.L = (b4 & 0x10) != 0
		state.ZL = (b4 & 0x20) != 0
		state.Minus = (b4 & 0x40) != 0
		state.LStickPress = (b4 & 0x80) != 0
	}

	if len(rep) > 5 {
		b5 := rep[5]
		state.Home = (b5 & 0x01) != 0
		state.Capture = (b5 & 0x02) != 0
		state.PaddleRight = (b5 & 0x04) != 0
		state.PaddleLeft = (b5 & 0x08) != 0
	}

	// Parse joysticks
	if len(rep) > 0 {
		reportID := rep[0]
		state.Joysticks = r.parseJoysticks(rep, reportID)
	}

	return state
}

func (r *HIDReader) parseJoysticks(data []byte, reportID byte) JoystickValues {
	vals := JoystickValues{}

	// Get raw 12-bit values
	lxRaw, lyRaw := getStickValues(data, true, reportID)
	rxRaw, ryRaw := getStickValues(data, false, reportID)

	// Normalize
	if lxRaw >= 0 && lyRaw >= 0 {
		vals.LX = r.normalizeAxis(lxRaw, r.calibration.LXCenter, r.calibration.LXMin, r.calibration.LXMax)
		vals.LY = r.normalizeAxis(lyRaw, r.calibration.LYCenter, r.calibration.LYMin, r.calibration.LYMax)
	}

	if rxRaw >= 0 && ryRaw >= 0 {
		vals.RX = r.normalizeAxis(rxRaw, r.calibration.RXCenter, r.calibration.RXMin, r.calibration.RXMax)
		vals.RY = r.normalizeAxis(ryRaw, r.calibration.RYCenter, r.calibration.RYMin, r.calibration.RYMax)
	}

	return vals
}

func (r *HIDReader) normalizeAxis(rawValue int, center, minVal, maxVal int) float64 {
	// Apply deadzone
	if abs(rawValue-center) < r.calibration.Deadzone {
		return 0.0
	}

	if rawValue > center {
		rangeMax := maxVal - center
		if rangeMax == 0 {
			return 0.0
		}
		return math.Min(1.0, float64(rawValue-center)/float64(rangeMax))
	}

	if rawValue < center {
		rangeMin := center - minVal
		if rangeMin == 0 {
			return 0.0
		}
		return math.Max(-1.0, float64(rawValue-center)/float64(rangeMin))
	}

	return 0.0
}

// getStickValues decodes 12-bit joystick values from HID report
func getStickValues(data []byte, isLeft bool, reportID byte) (int, int) {
	var offset int

	if reportID == 0x30 || reportID == 0x09 {
		if isLeft {
			offset = 6
		} else {
			offset = 9
		}
	} else {
		return -1, -1
	}

	if len(data) < offset+3 {
		return -1, -1
	}

	b0 := data[offset]
	b1 := data[offset+1]
	b2 := data[offset+2]

	// X is lower 12 bits, Y is upper 12 bits
	x := int(b0) | (int(b1&0x0F) << 8)
	y := (int(b1&0xF0) >> 4) | (int(b2) << 4)

	return x, y
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// Helper methods for ControllerState

// ButtonsEqual checks if button states are equal (ignoring joysticks)
func (s ControllerState) ButtonsEqual(o ControllerState) bool {
	return s.A == o.A && s.B == o.B && s.X == o.X && s.Y == o.Y &&
		s.L == o.L && s.R == o.R && s.ZL == o.ZL && s.ZR == o.ZR &&
		s.DpadUp == o.DpadUp && s.DpadDown == o.DpadDown &&
		s.DpadLeft == o.DpadLeft && s.DpadRight == o.DpadRight &&
		s.Plus == o.Plus && s.Minus == o.Minus &&
		s.Home == o.Home && s.Capture == o.Capture &&
		s.LStickPress == o.LStickPress && s.RStickPress == o.RStickPress &&
		s.PaddleLeft == o.PaddleLeft && s.PaddleRight == o.PaddleRight
}

// JoysticksChanged checks if joysticks moved significantly
func (s ControllerState) JoysticksChanged(o ControllerState, threshold float64) bool {
	return math.Abs(s.Joysticks.LX-o.Joysticks.LX) > threshold ||
		math.Abs(s.Joysticks.LY-o.Joysticks.LY) > threshold ||
		math.Abs(s.Joysticks.RX-o.Joysticks.RX) > threshold ||
		math.Abs(s.Joysticks.RY-o.Joysticks.RY) > threshold
}

// GetPressedButtons returns a list of pressed button names
func (s ControllerState) GetPressedButtons() []string {
	var pressed []string

	if s.A {
		pressed = append(pressed, "A")
	}
	if s.B {
		pressed = append(pressed, "B")
	}
	if s.X {
		pressed = append(pressed, "X")
	}
	if s.Y {
		pressed = append(pressed, "Y")
	}
	if s.L {
		pressed = append(pressed, "L")
	}
	if s.R {
		pressed = append(pressed, "R")
	}
	if s.ZL {
		pressed = append(pressed, "ZL")
	}
	if s.ZR {
		pressed = append(pressed, "ZR")
	}
	if s.DpadUp {
		pressed = append(pressed, "↑")
	}
	if s.DpadDown {
		pressed = append(pressed, "↓")
	}
	if s.DpadLeft {
		pressed = append(pressed, "←")
	}
	if s.DpadRight {
		pressed = append(pressed, "→")
	}
	if s.Plus {
		pressed = append(pressed, "+")
	}
	if s.Minus {
		pressed = append(pressed, "-")
	}
	if s.Home {
		pressed = append(pressed, "HOME")
	}
	if s.Capture {
		pressed = append(pressed, "CAPTURE")
	}
	if s.LStickPress {
		pressed = append(pressed, "L-STICK")
	}
	if s.RStickPress {
		pressed = append(pressed, "R-STICK")
	}
	if s.PaddleLeft {
		pressed = append(pressed, "PADDLE-L")
	}
	if s.PaddleRight {
		pressed = append(pressed, "PADDLE-R")
	}

	return pressed
}

// GetStickDirection returns direction string for a stick
func GetStickDirection(x, y float64, deadzone float64) string {
	if math.Abs(x) < deadzone && math.Abs(y) < deadzone {
		return "CENTER"
	}
	if math.Abs(x) > math.Abs(y) {
		if x > 0 {
			return "RIGHT"
		}
		return "LEFT"
	}
	if y > 0 {
		return "DOWN"
	}
	return "UP"
}
