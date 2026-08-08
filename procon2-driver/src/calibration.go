package main

import (
	"fmt"
	"log"
	"math"
	"time"
)

// CalibrateJoysticks performs an interactive calibration process
// Returns a new JoystickCalibration with measured values
func CalibrateJoysticks(reader *HIDReader) (JoystickCalibration, error) {
	cal := JoystickCalibration{
		Deadzone: 50, // Keep default deadzone
	}

	fmt.Println("🎮 Joystick Calibration Wizard")
	fmt.Println("===============================\n")

	// Step 1: Center position
	fmt.Println("Step 1: CENTER POSITION")
	fmt.Println("➜ Let both sticks rest in their natural center position")
	fmt.Println("➜ Don't touch the sticks!")
	fmt.Print("\nPress ENTER when ready...")
	fmt.Scanln()

	centerSamples := 50
	fmt.Printf("Collecting %d samples...\n", centerSamples)

	lxSum, lySum, rxSum, rySum := 0, 0, 0, 0

	for i := 0; i < centerSamples; i++ {
		// Get raw values directly from HID data
		lx, ly, rx, ry, err := readRawStickValues(reader)
		if err != nil {
			return cal, err
		}

		lxSum += lx
		lySum += ly
		rxSum += rx
		rySum += ry

		time.Sleep(20 * time.Millisecond)
	}

	cal.LXCenter = lxSum / centerSamples
	cal.LYCenter = lySum / centerSamples
	cal.RXCenter = rxSum / centerSamples
	cal.RYCenter = rySum / centerSamples

	fmt.Printf("✅ Center values recorded:\n")
	fmt.Printf("   Left:  X=%d Y=%d\n", cal.LXCenter, cal.LYCenter)
	fmt.Printf("   Right: X=%d Y=%d\n\n", cal.RXCenter, cal.RYCenter)

	// Step 2: Full range motion
	fmt.Println("Step 2: FULL RANGE")
	fmt.Println("➜ Move BOTH sticks in complete circles")
	fmt.Println("➜ Push them all the way to the edges in ALL directions")
	fmt.Println("➜ Continue for about 5 seconds")
	fmt.Print("\nPress ENTER to start...")
	fmt.Scanln()

	duration := 5 * time.Second
	fmt.Printf("Recording for %v... GO!\n", duration)

	lxMin, lyMin, rxMin, ryMin := 4095, 4095, 4095, 4095
	lxMax, lyMax, rxMax, ryMax := 0, 0, 0, 0

	startTime := time.Now()
	sampleCount := 0

	for time.Since(startTime) < duration {
		lx, ly, rx, ry, err := readRawStickValues(reader)
		if err != nil {
			continue
		}

		// Track min/max
		if lx < lxMin {
			lxMin = lx
		}
		if lx > lxMax {
			lxMax = lx
		}
		if ly < lyMin {
			lyMin = ly
		}
		if ly > lyMax {
			lyMax = ly
		}
		if rx < rxMin {
			rxMin = rx
		}
		if rx > rxMax {
			rxMax = rx
		}
		if ry < ryMin {
			ryMin = ry
		}
		if ry > ryMax {
			ryMax = ry
		}

		sampleCount++

		// Progress indicator
		if sampleCount%20 == 0 {
			elapsed := time.Since(startTime)
			remaining := duration - elapsed
			fmt.Printf("\rRecording... %.1fs remaining", remaining.Seconds())
		}

		time.Sleep(20 * time.Millisecond)
	}

	fmt.Printf("\r✅ Range calibration complete! (%d samples)\n\n", sampleCount)

	// Set calibration values with some margin
	margin := 100
	cal.LXMin = maxInt(lxMin-margin, 0)
	cal.LXMax = minInt(lxMax+margin, 4095)
	cal.LYMin = maxInt(lyMin-margin, 0)
	cal.LYMax = minInt(lyMax+margin, 4095)
	cal.RXMin = maxInt(rxMin-margin, 0)
	cal.RXMax = minInt(rxMax+margin, 4095)
	cal.RYMin = maxInt(ryMin-margin, 0)
	cal.RYMax = minInt(ryMax+margin, 4095)

	// Display results
	fmt.Println("📊 Calibration Results:")
	fmt.Println("========================")
	fmt.Printf("\nLeft Stick:\n")
	fmt.Printf("  X: Center=%d, Min=%d, Max=%d (Range: %d)\n",
		cal.LXCenter, cal.LXMin, cal.LXMax, cal.LXMax-cal.LXMin)
	fmt.Printf("  Y: Center=%d, Min=%d, Max=%d (Range: %d)\n",
		cal.LYCenter, cal.LYMin, cal.LYMax, cal.LYMax-cal.LYMin)

	fmt.Printf("\nRight Stick:\n")
	fmt.Printf("  X: Center=%d, Min=%d, Max=%d (Range: %d)\n",
		cal.RXCenter, cal.RXMin, cal.RXMax, cal.RXMax-cal.RXMin)
	fmt.Printf("  Y: Center=%d, Min=%d, Max=%d (Range: %d)\n",
		cal.RYCenter, cal.RYMin, cal.RYMax, cal.RYMax-cal.RYMin)

	fmt.Printf("\nDeadzone: %d\n\n", cal.Deadzone)

	// Generate code output
	fmt.Println("📋 Copy this calibration to your code:")
	fmt.Println("=======================================")
	fmt.Printf(`var MyCalibration = JoystickCalibration{
	LXCenter: %d, LXMin: %d, LXMax: %d,
	LYCenter: %d, LYMin: %d, LYMax: %d,
	RXCenter: %d, RXMin: %d, RXMax: %d,
	RYCenter: %d, RYMin: %d, RYMax: %d,
	Deadzone: %d,
}
`, cal.LXCenter, cal.LXMin, cal.LXMax,
		cal.LYCenter, cal.LYMin, cal.LYMax,
		cal.RXCenter, cal.RXMin, cal.RXMax,
		cal.RYCenter, cal.RYMin, cal.RYMax,
		cal.Deadzone)

	return cal, nil
}

// readRawStickValues reads raw 12-bit joystick values from HID report
func readRawStickValues(reader *HIDReader) (lx, ly, rx, ry int, err error) {
	// Read a fresh report
	n, readErr := reader.file.Read(reader.buffer[:])
	if readErr != nil {
		return 0, 0, 0, 0, fmt.Errorf("read error: %w", readErr)
	}

	if n < 12 {
		return 0, 0, 0, 0, fmt.Errorf("report too short: %d bytes", n)
	}

	data := reader.buffer[:n]
	reportID := data[0]

	// Extract raw stick values using the existing function
	lx, ly = getStickValues(data, true, reportID)
	rx, ry = getStickValues(data, false, reportID)

	if lx < 0 || ly < 0 || rx < 0 || ry < 0 {
		return 0, 0, 0, 0, fmt.Errorf("invalid stick values")
	}

	return lx, ly, rx, ry, nil
}

// TestCalibration shows live joystick values using the new calibration
func TestCalibration(reader *HIDReader, cal JoystickCalibration) error {
	fmt.Println("\n🧪 Testing Calibration")
	fmt.Println("=====================")
	fmt.Println("Move the sticks around to verify calibration")
	fmt.Println("Values should range from -1.0 to +1.0")
	fmt.Println("Center should be close to 0.0")
	fmt.Println("\nPress CTRL+C to exit\n")

	// Create temporary reader with new calibration
	reader.calibration = cal

	lastPrint := time.Now()

	for {
		state, err := reader.ReadStateTimeout(100 * time.Millisecond)
		if err != nil {
			continue
		}

		// Throttle output to avoid spam
		if time.Since(lastPrint) < 100*time.Millisecond {
			continue
		}
		lastPrint = time.Now()

		j := state.Joysticks

		// Show normalized values and check if they're in valid range
		lxStatus := getStatusIcon(j.LX)
		lyStatus := getStatusIcon(j.LY)
		rxStatus := getStatusIcon(j.RX)
		ryStatus := getStatusIcon(j.RY)

		output := fmt.Sprintf(
			"L: %s %+.3f, %s %+.3f | R: %s %+.3f, %s %+.3f",
			lxStatus, j.LX, lyStatus, j.LY,
			rxStatus, j.RX, ryStatus, j.RY,
		)

		fmt.Printf("\r%-80s", output)
	}
}

func getStatusIcon(value float64) string {
	absVal := math.Abs(value)
	if absVal < 0.05 {
		return "⚪" // Near center
	} else if absVal < 0.95 {
		return "🟢" // Normal range
	} else if absVal <= 1.0 {
		return "🟡" // Edge
	}
	return "🔴" // Out of range (bad calibration)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// RunCalibrationWizard is a convenience function to run the full calibration process
func RunCalibrationWizard(hidPath string) error {
	log.Println("Opening controller for calibration...")

	// Open with default calibration (we'll replace it)
	reader, err := NewHIDReader(hidPath, DefaultCalibration)
	if err != nil {
		return fmt.Errorf("failed to open HID device: %w", err)
	}
	defer reader.Close()

	// Run calibration
	newCal, err := CalibrateJoysticks(reader)
	if err != nil {
		return fmt.Errorf("calibration failed: %w", err)
	}

	// Ask if user wants to test
	fmt.Print("\nWould you like to test the calibration? (y/n): ")
	var response string
	fmt.Scanln(&response)

	if response == "y" || response == "Y" {
		if err := TestCalibration(reader, newCal); err != nil {
			return err
		}
	}

	return nil
}

// QuickCalibrate performs a fast calibration and returns the new calibration values
// This is meant to be called programmatically without user prompts
func QuickCalibrate(reader *HIDReader) (JoystickCalibration, error) {
	cal := JoystickCalibration{
		Deadzone: 50,
	}

	log.Println("Starting quick calibration...")

	// Step 1: Measure center (2 seconds)
	log.Println("Measuring center position (keep sticks centered)...")
	centerSamples := 50
	lxSum, lySum, rxSum, rySum := 0, 0, 0, 0

	for i := 0; i < centerSamples; i++ {
		lx, ly, rx, ry, err := readRawStickValues(reader)
		if err != nil {
			return cal, fmt.Errorf("center calibration error: %w", err)
		}
		lxSum += lx
		lySum += ly
		rxSum += rx
		rySum += ry
		time.Sleep(40 * time.Millisecond)
	}

	cal.LXCenter = lxSum / centerSamples
	cal.LYCenter = lySum / centerSamples
	cal.RXCenter = rxSum / centerSamples
	cal.RYCenter = rySum / centerSamples

	log.Printf("Center recorded: L(%d,%d) R(%d,%d)", cal.LXCenter, cal.LYCenter, cal.RXCenter, cal.RYCenter)

	// Step 2: Measure range (5 seconds)
	log.Println("Measuring range (rotate both sticks in full circles)...")

	lxMin, lyMin, rxMin, ryMin := 4095, 4095, 4095, 4095
	lxMax, lyMax, rxMax, ryMax := 0, 0, 0, 0

	duration := 5 * time.Second
	startTime := time.Now()

	for time.Since(startTime) < duration {
		lx, ly, rx, ry, err := readRawStickValues(reader)
		if err != nil {
			continue
		}

		if lx < lxMin {
			lxMin = lx
		}
		if lx > lxMax {
			lxMax = lx
		}
		if ly < lyMin {
			lyMin = ly
		}
		if ly > lyMax {
			lyMax = ly
		}
		if rx < rxMin {
			rxMin = rx
		}
		if rx > rxMax {
			rxMax = rx
		}
		if ry < ryMin {
			ryMin = ry
		}
		if ry > ryMax {
			ryMax = ry
		}

		time.Sleep(40 * time.Millisecond)
	}

	// Set with margin
	margin := 100
	cal.LXMin = maxInt(lxMin-margin, 0)
	cal.LXMax = minInt(lxMax+margin, 4095)
	cal.LYMin = maxInt(lyMin-margin, 0)
	cal.LYMax = minInt(lyMax+margin, 4095)
	cal.RXMin = maxInt(rxMin-margin, 0)
	cal.RXMax = minInt(rxMax+margin, 4095)
	cal.RYMin = maxInt(ryMin-margin, 0)
	cal.RYMax = minInt(ryMax+margin, 4095)

	log.Printf("✅ Calibration complete: L(X:%d-%d, Y:%d-%d) R(X:%d-%d, Y:%d-%d)",
		cal.LXMin, cal.LXMax, cal.LYMin, cal.LYMax,
		cal.RXMin, cal.RXMax, cal.RYMin, cal.RYMax)

	return cal, nil
}
