package main

import (
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"time"
)

// DisplayOptions configures display behavior
type DisplayOptions struct {
	ShowRawValues bool
	ShowDirection bool
	UpdateRate    time.Duration
}

// InputMonitor monitors and displays controller input
type InputMonitor struct {
	reader    *HIDReader
	opts      DisplayOptions
	lastState ControllerState
}

// NewInputMonitor creates a new input monitor
func NewInputMonitor(reader *HIDReader, opts DisplayOptions) *InputMonitor {
	return &InputMonitor{
		reader: reader,
		opts:   opts,
	}
}

// RunButtonMonitor displays button presses and joystick positions
func (m *InputMonitor) RunButtonMonitor() error {
	fmt.Println("📡 Controller Input Monitor")
	fmt.Println("✅ Ready! Press buttons and move joysticks.")
	fmt.Println("Press CTRL+C to quit.\n")

	const changeThreshold = 50.0 // threshold in raw units

	for {
		state, err := m.reader.ReadState()
		if err != nil {
			continue
		}

		// Check if anything changed
		buttonsChanged := !state.ButtonsEqual(m.lastState)
		joysticksChanged := state.JoysticksChanged(m.lastState, changeThreshold)

		if buttonsChanged || joysticksChanged {
			output := m.formatState(state)
			fmt.Printf("\r\033[K%s", output)
			m.lastState = state
		}

		time.Sleep(m.opts.UpdateRate)
	}
}

// RunStickViewer displays detailed joystick information
func (m *InputMonitor) RunStickViewer() error {
	fmt.Println("📡 Joystick Viewer")
	fmt.Println("✅ Move both sticks to see detailed values")
	fmt.Println("Press CTRL+C to quit.\n")

	for {
		state, err := m.reader.ReadState()
		if err != nil {
			continue
		}

		output := m.formatJoysticks(state)
		fmt.Printf("\r\033[K%s", output)
		time.Sleep(m.opts.UpdateRate)
	}
}

// formatState formats the complete controller state
func (m *InputMonitor) formatState(state ControllerState) string {
	var parts []string

	// Format buttons
	pressed := state.GetPressedButtons()
	if len(pressed) > 0 {
		parts = append(parts, "Pressed: "+strings.Join(pressed, " + "))
	}

	// Format joysticks (raw ints)
	j := state.Joysticks
	joystickStr := fmt.Sprintf(
		"L-XY: (%4d, %4d) | R-XY: (%4d, %4d)",
		j.LX, j.LY, j.RX, j.RY,
	)
	parts = append(parts, joystickStr)

	if len(parts) == 0 {
		return "Ready..."
	}

	return strings.Join(parts, " | ")
}

// formatJoysticks formats detailed joystick information
func (m *InputMonitor) formatJoysticks(state ControllerState) string {
	j := state.Joysticks

	// Convert raw ints to float64 for direction helper
	lx := float64(j.LX)
	ly := float64(j.LY)
	rx := float64(j.RX)
	ry := float64(j.RY)

	// Deadzone in raw units (example: 300 ~ small deadzone)
	deadzone := 300.0

	lDir := GetStickDirection(lx, ly, deadzone)
	rDir := GetStickDirection(rx, ry, deadzone)

	output := fmt.Sprintf(
		"L(%4d, %4d) %-8s | R(%4d, %4d) %-8s",
		j.LX, j.LY, lDir,
		j.RX, j.RY, rDir,
	)

	if m.opts.ShowRawValues {
		rawStr := fmt.Sprintf(" | RAW L(%4d,%4d) R(%4d,%4d)", j.LX, j.LY, j.RX, j.RY)
		output += rawStr
	}

	return output
}

// DebugMonitor displays debug information
type DebugMonitor struct {
	reader *HIDReader
}

// NewDebugMonitor creates a debug monitor
func NewDebugMonitor(reader *HIDReader) *DebugMonitor {
	return &DebugMonitor{reader: reader}
}

// Run collects and displays debug information
func (d *DebugMonitor) Run(numReports int) error {
	fmt.Printf("📊 Debug Mode: Collecting %d reports\n", numReports)
	fmt.Println("Move sticks and press buttons during collection...")
	fmt.Println()

	debug, err := d.reader.DebugReport(numReports)
	if err != nil {
		return err
	}

	// Display first report
	if len(debug.Reports) > 0 {
		log.Printf("First report (%d bytes): %s",
			len(debug.Reports[0]),
			hex.EncodeToString(debug.Reports[0]),
		)
	}

	// Display statistics
	fmt.Println("\n📈 Byte Statistics:")
	fmt.Println("Idx | Changes | Min | Max | Range")
	fmt.Println("----|---------|------|------|------")

	for i, stats := range debug.Stats {
		if !stats.Seen {
			continue
		}
		rangeVal := int(stats.Max) - int(stats.Min)
		fmt.Printf("%3d | %7d | 0x%02x | 0x%02x | %3d\n",
			i, stats.Changes, stats.Min, stats.Max, rangeVal)
	}

	fmt.Println("\n✅ Debug collection complete")
	return nil
}

// Utility function for joining strings
func joinStrings(items []string, sep string) string {
	if len(items) == 0 {
		return ""
	}
	return strings.Join(items, sep)
}
