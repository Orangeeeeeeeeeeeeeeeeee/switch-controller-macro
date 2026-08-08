# [Release 0.0.2](https://github.com/dalmatheo/procon2-driver/releases/tag/v0.0.2)

## Additions

/

## Deletions

/

## Fixes

- Struct-Level Buffer Hoisting: Refactored the HapticPlayer and HIDReader structs to include pre-allocated fixed-size buffers. This moves memory allocation from the "hot path" (execution time) to initialization time. -> [@dalmatheo](https://github.com/dalmatheo)
- Zero-Allocation Haptics: The Play function now reuses a single [64]byte internal buffer for all vibration frames instead of allocating new slices on every ticker interval. -> [@dalmatheo](https://github.com/dalmatheo)
- Fast Memory Operations: Replaced manual copy loops with the built-in copy() function for faster, CPU-optimized memory transfers between buffers. -> [@dalmatheo](https://github.com/dalmatheo)

## Links

**Full Changelog**: https://github.com/dalmatheo/procon2-driver/compare/v0.0.1...v0.0.2

___

# [Release 0.0.1](https://github.com/dalmatheo/procon2-driver/releases/tag/v0.0.1)

## Additions

- Multi-Controller Manager: Added a system to track and manage up to 4 controllers via USB Bus/Address -> [@dalmatheo](https://github.com/dalmatheo)
- Virtual Gamepad (UInput): Implemented VirtualGamepad to map raw HID reports to system-level Linux input events -> [@dalmatheo](https://github.com/dalmatheo)
- Haptic Feedback Engine: Added a HapticPlayer to send vibration patterns using the 0x50 subcommand -> [@dalmatheo](https://github.com/dalmatheo)
- Interactive Calibration: Created a calibration wizard to calculate joystick centers, mins, and maxes -> [@dalmatheo](https://github.com/dalmatheo)
- HID Discovery Logic: Added GetHidrawForUSB and GetEvdevForUSB to find device nodes by walking the sysfs tree -> [@dalmatheo](https://github.com/dalmatheo)
- Debug Input Monitor: Added a real-time CLI display for button states and normalized joystick values -> [@dalmatheo](https://github.com/dalmatheo)

## Deletions

/

## Fixes

/

## Links

**Full Changelog**: / (first version)
