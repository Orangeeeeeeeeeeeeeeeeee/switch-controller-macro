package main

import (
	"fmt"
	"log"
	"time"

	"github.com/google/gousb"
)

const (
	VendorID = 0x057E
	// Product IDs maintained for reference, though discovery is now strictly VID/PID based
	ProductProcon      = 0x2069
	USBInterfaceNumber = 1
)

// Controller represents a connected Nintendo controller
type Controller struct {
	device    *gousb.Device
	iface     *gousb.Interface
	epOut     *gousb.OutEndpoint
	epIn      *gousb.InEndpoint
	hidPath   string
	packetID  byte
	outBuffer [64]byte
}

// NewController accepts an already open USB device and initializes the interface
func NewController(dev *gousb.Device, configNum, ifaceNum int) (*Controller, error) {
	intf, epOut, epIn, err := claimInterface(dev, configNum, ifaceNum)
	if err != nil {
		return nil, fmt.Errorf("failed to claim interface: %w", err)
	}

	// Resolve hidraw path immediately for the Reader
	bus := dev.Desc.Bus
	addr := dev.Desc.Address
	hidPath, err := GetHidrawForUSB(int(bus), int(addr))
	if err != nil {
		log.Printf("⚠️ Warning: Could not find hidraw node for Bus %d Addr %d: %v", bus, addr, err)
	}

	return &Controller{
		device:  dev,
		iface:   intf,
		epOut:   epOut,
		epIn:    epIn,
		hidPath: hidPath,
	}, nil
}

func (c *Controller) Close() error {
	if c.iface != nil {
		c.iface.Close()
	}
	// We do not close c.device here as it is managed by the main loop context
	// but strictly speaking, gousb devices should be closed.
	// The Manager will handle the device closure.
	return nil
}

func (c *Controller) GetHIDPath() string {
	return c.hidPath
}

// SetPlayerLEDs sets the controller LEDs (Player 1-4) using standard Pro Controller commands
func (c *Controller) SetPlayerLEDs(playerNum int) error {
	var ledPattern byte
	switch playerNum {
	case 1:
		ledPattern = 0x01 // ●○○○
	case 2:
		ledPattern = 0x02 // ○●○○
	case 3:
		ledPattern = 0x04 // ○○●○
	case 4:
		ledPattern = 0x08 // ○○○●
	default:
		ledPattern = 0x01
	}

	// Subcommand 0x30: Set Player Lights
	return c.SendSubcommand(0x30, []byte{ledPattern})
}

// SendSubcommand sends a standard Pro Controller output report (0x01)
func (c *Controller) SendSubcommand(subcmd byte, data []byte) error {
	for i := range c.outBuffer {
		c.outBuffer[i] = 0
	}

	c.packetID = (c.packetID + 1) & 0x0F

	c.outBuffer[0] = 0x01 // Output Report ID
	c.outBuffer[1] = c.packetID

	// Rumble data (Low rumble neutral)
	c.outBuffer[2] = 0x00
	c.outBuffer[3] = 0x01
	c.outBuffer[4] = 0x40
	c.outBuffer[5] = 0x40
	c.outBuffer[6] = 0x00
	c.outBuffer[7] = 0x01
	c.outBuffer[8] = 0x40
	c.outBuffer[9] = 0x40

	c.outBuffer[10] = subcmd
	copy(c.outBuffer[11:], data)

	if c.epOut != nil {
		_, err := c.epOut.Write(c.outBuffer[:])
		return err
	}
	return fmt.Errorf("output endpoint not connected")
}

// SendInitSequence sends the initialization packets
// (Preserving your original sequence for compatibility with your device)
func (c *Controller) SendInitSequence() error {
	packets := [][]byte{
		{0x03, 0x91, 0x00, 0x0d, 0x00, 0x08, 0x00, 0x00, 0x01, 0x00, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
		{0x07, 0x91, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00},
		{0x16, 0x91, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00},
		{0x15, 0x91, 0x00, 0x01, 0x00, 0x0e, 0x00, 0x00, 0x00, 0x02, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
		{0x15, 0x91, 0x00, 0x02, 0x00, 0x11, 0x00, 0x00, 0x00, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
		{0x15, 0x91, 0x00, 0x03, 0x00, 0x01, 0x00, 0x00, 0x00},
		{0x09, 0x91, 0x00, 0x07, 0x00, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		{0x0c, 0x91, 0x00, 0x02, 0x00, 0x04, 0x00, 0x00, 0x27, 0x00, 0x00, 0x00},
		{0x11, 0x91, 0x00, 0x03, 0x00, 0x00, 0x00, 0x00},
		{0x0a, 0x91, 0x00, 0x08, 0x00, 0x14, 0x00, 0x00, 0x01, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x35, 0x00, 0x46, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		{0x0c, 0x91, 0x00, 0x04, 0x00, 0x04, 0x00, 0x00, 0x27, 0x00, 0x00, 0x00},
		{0x03, 0x91, 0x00, 0x0a, 0x00, 0x04, 0x00, 0x00, 0x09, 0x00, 0x00, 0x00},
		{0x10, 0x91, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00},
		{0x01, 0x91, 0x00, 0x0c, 0x00, 0x00, 0x00, 0x00},
		{0x03, 0x91, 0x00, 0x01, 0x00, 0x00, 0x00},
		{0x0a, 0x91, 0x00, 0x02, 0x00, 0x04, 0x00, 0x00, 0x03, 0x00, 0x00},
		{0x09, 0x91, 0x00, 0x07, 0x00, 0x08, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
	}

	log.Println("Sending initialization sequence...")
	for i, p := range packets {
		if c.epOut != nil {
			if _, err := c.epOut.Write(p); err != nil {
				log.Printf("Failed to write packet %d: %v", i+1, err)
			}
			time.Sleep(15 * time.Millisecond) // Slight delay between packets

			// Try to drain input to prevent buffer overflow
			if c.epIn != nil {
				buf := make([]byte, 64)
				c.epIn.Read(buf)
			}
		}
	}
	return nil
}

func claimInterface(dev *gousb.Device, configNum int, ifaceNum int) (*gousb.Interface, *gousb.OutEndpoint, *gousb.InEndpoint, error) {
	cfg, err := dev.Config(configNum)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to open config %d: %w", configNum, err)
	}

	intf, err := cfg.Interface(ifaceNum, 0)
	if err != nil {
		cfg.Close()
		return nil, nil, nil, fmt.Errorf("failed to claim interface %d: %w", ifaceNum, err)
	}

	var epOut *gousb.OutEndpoint
	var epIn *gousb.InEndpoint

	for _, e := range intf.Setting.Endpoints {
		if e.Direction == gousb.EndpointDirectionOut && e.TransferType == gousb.TransferTypeBulk {
			epOut, err = intf.OutEndpoint(e.Number)
			if err != nil {
				intf.Close()
				return nil, nil, nil, err
			}
		}
		if e.Direction == gousb.EndpointDirectionIn && (e.TransferType == gousb.TransferTypeInterrupt || e.TransferType == gousb.TransferTypeBulk) {
			epIn, err = intf.InEndpoint(e.Number)
			if err != nil {
				intf.Close()
				return nil, nil, nil, err
			}
		}
	}

	return intf, epOut, epIn, nil
}
