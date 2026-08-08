package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/google/gousb"
)

const (
	MaxPlayers    = 4
	DRIVER_NAME   = "Nintendo Pro Controller 2"
	PROCON_VENDOR = 0x057e
	EVIOCGRAB     = 0x40044590
)

// ActiveDriver represents a running controller instance
type ActiveDriver struct {
	Driver    *Driver
	USBDevice *gousb.Device
	Slot      int    // 0 to 3 (Player 1-4)
	UniqueID  string // "Bus-Addr"
	StopChan  chan struct{}
	WG        sync.WaitGroup
	GrabFile  *os.File // Handle to the grabbed evdev node
}

// Manager handles detection and lifecycle of controllers
type Manager struct {
	ctx     *gousb.Context
	drivers map[string]*ActiveDriver
	slots   [MaxPlayers]bool
	mu      sync.Mutex
}

func NewManager(ctx *gousb.Context) *Manager {
	return &Manager{
		ctx:     ctx,
		drivers: make(map[string]*ActiveDriver),
	}
}

// Scan looks for new devices and starts drivers for them
func (m *Manager) Scan() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Iterate all USB devices matching Nintendo VID
	devs, err := m.ctx.OpenDevices(func(desc *gousb.DeviceDesc) bool {
		// Filter by VendorID
		if desc.Vendor != gousb.ID(PROCON_VENDOR) {
			return false
		}

		// Accept standard Product IDs and specific clones
		// You can add more IDs here if needed
		if desc.Product == 0x2009 || desc.Product == 0x2019 || desc.Product == 0x2069 {
			return true
		}
		return false
	})

	if err != nil {
		log.Printf("Error scanning USB: %v", err)
		return
	}

	for _, dev := range devs {
		bus := dev.Desc.Bus
		addr := dev.Desc.Address
		uid := fmt.Sprintf("%d-%d", bus, addr)

		// Check if we already manage this device
		if _, exists := m.drivers[uid]; exists {
			dev.Close() // Already running, close this duplicate handle
			continue
		}

		// Found a new device! Find a slot.
		slot := m.findFreeSlot()
		if slot == -1 {
			log.Printf("⚠️ Found device at %s but all %d player slots are full.", uid, MaxPlayers)
			dev.Close()
			continue
		}

		log.Printf("✨ New Controller found: %s -> Assigning Player %d", uid, slot+1)

		// Start the driver
		ad, err := m.startDriver(dev, slot, uid)
		if err != nil {
			log.Printf("❌ Failed to start driver for %s: %v", uid, err)
			dev.Close()
			m.slots[slot] = false
		} else {
			m.drivers[uid] = ad
		}
	}
}

func (m *Manager) findFreeSlot() int {
	for i := 0; i < MaxPlayers; i++ {
		if !m.slots[i] {
			m.slots[i] = true
			return i
		}
	}
	return -1
}

func (m *Manager) startDriver(dev *gousb.Device, slotIndex int, uid string) (*ActiveDriver, error) {
	// 1. Initialize Controller (USB)
	ctrl, err := NewController(dev, 1, 1) // Config 1, Interface 1
	if err != nil {
		return nil, err
	}

	// 2. Exclusive Grab of original evdev node to hide it
	var grabFile *os.File
	evdevPath, err := GetEvdevForUSB(int(dev.Desc.Bus), int(dev.Desc.Address))
	if err == nil {
		f, err := os.OpenFile(evdevPath, os.O_RDONLY, 0)
		if err == nil {
			if err := ioctl(f.Fd(), EVIOCGRAB, 1); err == nil {
				grabFile = f
				log.Printf("🔒 Grabbed original evdev: %s", evdevPath)
			} else {
				f.Close()
			}
		}
	} else {
		log.Printf("Note: Could not find evdev to grab: %v", err)
	}

	// 3. Send Init Sequence
	if err := ctrl.SendInitSequence(); err != nil {
		ctrl.Close()
		return nil, fmt.Errorf("init failed: %w", err)
	}

	// 4. Set LEDs (Player Number)
	// We wait a moment after init before setting LEDs
	time.Sleep(100 * time.Millisecond)
	ctrl.SetPlayerLEDs(slotIndex + 1)

	// 5. Setup HID Reader
	if ctrl.GetHIDPath() == "" {
		ctrl.Close()
		return nil, fmt.Errorf("no HID path found")
	}
	reader, err := NewHIDReader(ctrl.GetHIDPath(), DefaultCalibration)
	if err != nil {
		ctrl.Close()
		return nil, err
	}

	// 6. Setup Virtual Gamepad (uinput)
	virtual, err := NewVirtualGamepad(slotIndex + 1)
	if err != nil {
		reader.Close()
		ctrl.Close()
		return nil, err
	}

	d := &Driver{
		controller: ctrl,
		reader:     reader,
		virtual:    virtual,
	}

	ad := &ActiveDriver{
		Driver:    d,
		USBDevice: dev,
		Slot:      slotIndex,
		UniqueID:  uid,
		StopChan:  make(chan struct{}),
		GrabFile:  grabFile,
	}

	ad.WG.Add(1)
	go func() {
		defer ad.WG.Done()
		m.driverLoop(ad)
	}()

	return ad, nil
}

func (m *Manager) driverLoop(ad *ActiveDriver) {
	log.Printf("🎮 Player %d connected and running", ad.Slot+1)

	defer func() {
		log.Printf("🔌 Player %d (%s) disconnected", ad.Slot+1, ad.UniqueID)

		// Cleanup resources
		if ad.GrabFile != nil {
			ioctl(ad.GrabFile.Fd(), EVIOCGRAB, 0)
			ad.GrabFile.Close()
		}
		ad.Driver.Close()
		// ad.USBDevice is closed by ad.Driver.Close() implicitly or manually here
		ad.USBDevice.Close()

		m.mu.Lock()
		delete(m.drivers, ad.UniqueID)
		m.slots[ad.Slot] = false
		m.mu.Unlock()
	}()

	ticker := time.NewTicker(2 * time.Millisecond)
	defer ticker.Stop()

	failCount := 0

	for {
		select {
		case <-ad.StopChan:
			return
		case <-ticker.C:
			state, err := ad.Driver.reader.ReadStateTimeout(100 * time.Millisecond)
			if err != nil {
				failCount++
				if failCount > 20 { // ~2 seconds of failure
					log.Printf("Player %d read timeout/error: %v", ad.Slot+1, err)
					return // Exit loop, triggers cleanup
				}
				continue
			}
			failCount = 0
			ad.Driver.virtual.Update(state)
		}
	}
}

func (m *Manager) Cleanup() {
	m.mu.Lock()
	drivers := make([]*ActiveDriver, 0, len(m.drivers))
	for _, ad := range m.drivers {
		drivers = append(drivers, ad)
		close(ad.StopChan)
	}
	m.mu.Unlock()

	for _, ad := range drivers {
		ad.WG.Wait()
	}
}

// Driver struct wrapper
type Driver struct {
	controller *Controller
	reader     *HIDReader
	virtual    *VirtualGamepad
}

func (d *Driver) Close() {
	if d.virtual != nil {
		d.virtual.Close()
	}
	if d.reader != nil {
		d.reader.Close()
	}
	if d.controller != nil {
		d.controller.Close()
	}
}

// NewVirtualGamepad creates a new virtual gamepad with Player Number in name
func NewVirtualGamepad(playerNum int) (*VirtualGamepad, error) {
	f, err := os.OpenFile("/dev/uinput", os.O_WRONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to open /dev/uinput: %w", err)
	}

	// Basic Setup (Keys, Axes, etc) - Same as original
	ioctl(f.Fd(), uiSetEvBit, uintptr(evKey))
	ioctl(f.Fd(), uiSetEvBit, uintptr(evAbs))
	ioctl(f.Fd(), uiSetEvBit, uintptr(evSyn))

	buttons := []uint16{
		btnSouth, btnEast, btnNorth, btnWest,
		btnTL, btnTR, btnTL2, btnTR2,
		btnSelect, btnStart, btnMode,
		btnThumbL, btnThumbR,
		btnDpadUp, btnDpadDown, btnDpadLeft, btnDpadRight,
	}
	for _, btn := range buttons {
		ioctl(f.Fd(), uiSetKeyBit, uintptr(btn))
	}

	axes := []uint16{absX, absY, absRX, absRY}
	for _, ax := range axes {
		ioctl(f.Fd(), uiSetAbsBit, uintptr(ax))
	}

	// Device Setup with Naming
	var usetup uinputSetup
	name := fmt.Sprintf("%s (Player %d)", DRIVER_NAME, playerNum)
	copy(usetup.name[:], name)
	usetup.id.bustype = busUsb
	usetup.id.vendor = PROCON_VENDOR
	usetup.id.product = 0x2019
	usetup.id.version = 1

	if err := ioctlSetup(f.Fd(), uiDevSetup, unsafe.Pointer(&usetup)); err != nil {
		f.Close()
		return nil, fmt.Errorf("UI_DEV_SETUP failed: %w", err)
	}

	// Axis Setup
	for _, ax := range axes {
		absSetup := uinputAbsSetup{
			code: ax,
			info: inputAbsinfo{
				min: -32768, max: 32767, fuzz: 16, flat: 128,
			},
		}
		ioctlSetup(f.Fd(), uiAbsSetup, unsafe.Pointer(&absSetup))
	}

	if err := ioctl(f.Fd(), uiDevCreate, 0); err != nil {
		f.Close()
		return nil, fmt.Errorf("UI_DEV_CREATE failed: %w", err)
	}

	return &VirtualGamepad{file: f, deadzone: 0.05}, nil
}

func main() {
	daemonMode := flag.Bool("daemon", false, "Run as daemon (stderr log)")
	calibrateMode := flag.Bool("calibrate", false, "Run calibration mode")
	flag.Parse()

	if *daemonMode {
		log.SetOutput(os.Stderr)
		log.SetFlags(0)
	} else {
		log.SetOutput(os.Stdout)
		log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	}

	// Calibration Mode
	if *calibrateMode {
		log.Println("🎮 Calibration Mode")
		log.Println("Plug in ONE controller to calibrate")

		ctx := gousb.NewContext()
		defer ctx.Close()

		// Find first Pro Controller
		devs, err := ctx.OpenDevices(func(desc *gousb.DeviceDesc) bool {
			if desc.Vendor != gousb.ID(PROCON_VENDOR) {
				return false
			}
			if desc.Product == 0x2009 || desc.Product == 0x2019 || desc.Product == 0x2069 {
				return true
			}
			return false
		})

		if err != nil || len(devs) == 0 {
			log.Fatal("No Pro Controller found. Please connect one.")
		}

		dev := devs[0]
		defer dev.Close()

		// Close other devices
		for i := 1; i < len(devs); i++ {
			devs[i].Close()
		}

		// Initialize controller
		ctrl, err := NewController(dev, 1, 1)
		if err != nil {
			log.Fatal("Failed to initialize controller:", err)
		}
		defer ctrl.Close()

		if err := ctrl.SendInitSequence(); err != nil {
			log.Fatal("Failed to send init sequence:", err)
		}

		time.Sleep(200 * time.Millisecond)

		if ctrl.GetHIDPath() == "" {
			log.Fatal("Could not find HID path for controller")
		}

		// Open reader with default calibration first
		reader, err := NewHIDReader(ctrl.GetHIDPath(), DefaultCalibration)
		if err != nil {
			log.Fatal("Failed to open HID reader:", err)
		}
		defer reader.Close()

		// Run quick calibration
		log.Println("\n📊 Step 1: Keep both sticks centered for 2 seconds...")
		time.Sleep(3 * time.Second) // Give user time to read

		log.Println("Step 2: Rotate both sticks in full circles for 5 seconds...")
		time.Sleep(1 * time.Second)

		newCal, err := QuickCalibrate(reader)
		if err != nil {
			log.Fatal("Calibration failed:", err)
		}

		// Print the code to use
		fmt.Println("\n✅ Calibration Complete!")
		fmt.Println("\n📋 Replace DefaultCalibration in hidinput.go with:")
		fmt.Println("==========================================")
		fmt.Printf(`var DefaultCalibration = JoystickCalibration{
	LXCenter: %d, LXMin: %d, LXMax: %d,
	LYCenter: %d, LYMin: %d, LYMax: %d,
	RXCenter: %d, RXMin: %d, RXMax: %d,
	RYCenter: %d, RYMin: %d, RYMax: %d,
	Deadzone: %d,
}
`, newCal.LXCenter, newCal.LXMin, newCal.LXMax,
			newCal.LYCenter, newCal.LYMin, newCal.LYMax,
			newCal.RXCenter, newCal.RXMin, newCal.RXMax,
			newCal.RYCenter, newCal.RYMin, newCal.RYMax,
			newCal.Deadzone)

		return
	}

	// Normal Driver Mode
	log.Println("🚀 Multi-Controller Driver Service Starting...")

	// Initialize USB Context
	ctx := gousb.NewContext()
	defer ctx.Close()

	// Initialize Manager
	manager := NewManager(ctx)

	// Signal Handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Scanning Loop
	go func() {
		for {
			manager.Scan()
			time.Sleep(2 * time.Second)
		}
	}()

	log.Println("✅ Service Ready. Waiting for controllers...")
	<-sigChan
	log.Println("\n🛑 Shutdown signal received. Cleaning up...")
	manager.Cleanup()
	log.Println("👋 Done.")
}

// --- UInput Constants (Must be present in main.go) ---
const (
	uiSetEvBit   = 0x40045564
	uiSetKeyBit  = 0x40045565
	uiSetAbsBit  = 0x40045567
	uiDevSetup   = 0x405c5503
	uiDevCreate  = 0x5501
	uiDevDestroy = 0x5502
	uiAbsSetup   = 0x401c5504

	evSyn = 0x00
	evKey = 0x01
	evAbs = 0x03

	btnSouth     = 0x130
	btnEast      = 0x131
	btnNorth     = 0x133
	btnWest      = 0x134
	btnTL        = 0x136
	btnTR        = 0x137
	btnTL2       = 0x138
	btnTR2       = 0x139
	btnSelect    = 0x13a
	btnStart     = 0x13b
	btnMode      = 0x13c
	btnThumbL    = 0x13d
	btnThumbR    = 0x13e
	btnDpadUp    = 0x220
	btnDpadDown  = 0x221
	btnDpadLeft  = 0x222
	btnDpadRight = 0x223

	absX   = 0x00
	absY   = 0x01
	absRX  = 0x03
	absRY  = 0x04
	busUsb = 0x03
)

type VirtualGamepad struct {
	file      *os.File
	lastState ControllerState
	deadzone  float64
}

func (v *VirtualGamepad) Update(state ControllerState) error {
	v.sendButton(btnSouth, state.A)
	v.sendButton(btnEast, state.B)
	v.sendButton(btnNorth, state.X)
	v.sendButton(btnWest, state.Y)
	v.sendButton(btnTL, state.L)
	v.sendButton(btnTR, state.R)
	v.sendButton(btnTL2, state.ZL)
	v.sendButton(btnTR2, state.ZR)
	v.sendButton(btnDpadUp, state.DpadUp)
	v.sendButton(btnDpadDown, state.DpadDown)
	v.sendButton(btnDpadLeft, state.DpadLeft)
	v.sendButton(btnDpadRight, state.DpadRight)
	v.sendButton(btnStart, state.Plus)
	v.sendButton(btnSelect, state.Minus)
	v.sendButton(btnMode, state.Home)
	v.sendButton(btnThumbL, state.LStickPress)
	v.sendButton(btnThumbR, state.RStickPress)

	lx := v.applyDeadzone(state.Joysticks.LX)
	ly := v.applyDeadzone(-state.Joysticks.LY)
	rx := v.applyDeadzone(state.Joysticks.RX)
	ry := v.applyDeadzone(-state.Joysticks.RY)

	v.sendAxis(absX, int32(lx*32767))
	v.sendAxis(absY, int32(ly*32767))
	v.sendAxis(absRX, int32(rx*32767))
	v.sendAxis(absRY, int32(ry*32767))

	v.sendSync()
	v.lastState = state
	return nil
}

func (v *VirtualGamepad) sendButton(code uint16, pressed bool) {
	val := int32(0)
	if pressed {
		val = 1
	}
	v.writeEvent(evKey, code, val)
}
func (v *VirtualGamepad) sendAxis(code uint16, value int32) {
	v.writeEvent(evAbs, code, value)
}
func (v *VirtualGamepad) sendSync() {
	v.writeEvent(evSyn, 0, 0)
}
func (v *VirtualGamepad) writeEvent(typ, code uint16, value int32) {
	var tv syscall.Timeval
	syscall.Gettimeofday(&tv)
	event := inputEvent{time: tv, typ: typ, code: code, value: value}
	syscall.Write(int(v.file.Fd()), (*(*[unsafe.Sizeof(event)]byte)(unsafe.Pointer(&event)))[:])
}
func (v *VirtualGamepad) applyDeadzone(value float64) float64 {
	if value > -v.deadzone && value < v.deadzone {
		return 0.0
	}
	return value
}
func (v *VirtualGamepad) Close() error {
	if v.file != nil {
		ioctl(v.file.Fd(), uiDevDestroy, 0)
		return v.file.Close()
	}
	return nil
}

// UInput Structs
type inputEvent struct {
	time      syscall.Timeval
	typ, code uint16
	value     int32
}
type inputId struct {
	bustype, vendor, product, version uint16
}
type inputAbsinfo struct {
	value, min, max, fuzz, flat, resolution int32
}
type uinputAbsSetup struct {
	code uint16
	_    [2]byte
	info inputAbsinfo
	_    [4]byte
}
type uinputSetup struct {
	id           inputId
	name         [80]byte
	ffEffectsMax uint32
	absinfo      [0x40]uinputAbsSetup
}

func ioctl(fd uintptr, request uintptr, arg uintptr) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, request, arg)
	if errno != 0 {
		return errno
	}
	return nil
}
func ioctlSetup(fd uintptr, request uintptr, arg unsafe.Pointer) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, request, uintptr(arg))
	if errno != 0 {
		return errno
	}
	return nil
}
