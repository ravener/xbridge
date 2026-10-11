package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"os"
	"syscall"
	"time"
)

const (
	evSyn = 0x00
	evKey = 0x01
	evAbs = 0x03

	synReport = 0x00

	// Virtual Xbox-style button codes.
	btnSouth  = 0x130 // A
	btnEast   = 0x131 // B
	btnWest   = 0x134 // X
	btnNorth  = 0x133 // Y
	btnTL     = 0x136 // LB
	btnTR     = 0x137 // RB
	btnSelect = 0x13a // Back
	btnStart  = 0x13b
	btnMode   = 0x13c // Guide / Xbox button
	btnThumbL = 0x13d // L3
	btnThumbR = 0x13e // R3

	// Absolute axis codes.
	absX     = 0x00
	absY     = 0x01
	absZ     = 0x02 // Left trigger
	absRX    = 0x03
	absRY    = 0x04
	absRZ    = 0x05 // Right trigger
	absHat0X = 0x10
	absHat0Y = 0x11

	busUSB = 0x03

	uiDevCreate  = 0x5501
	uiDevDestroy = 0x5502

	uiSetEvBit  = 0x40045564
	uiSetKeyBit = 0x40045565
	uiSetAbsBit = 0x40045567

	eviocGrab = 0x40044590
)

// Physical controller event codes.
const (
	physicalTriangle = 0x120
	physicalCircle   = 0x121
	physicalX        = 0x122
	physicalSquare   = 0x123
	physicalL1       = 0x124
	physicalR1       = 0x125
	physicalL2       = 0x126
	physicalR2       = 0x127
	physicalSelect   = 0x128
	physicalStart    = 0x129
	physicalL3       = 0x12a
	physicalR3       = 0x12b
)

type inputID struct {
	BusType uint16
	Vendor  uint16
	Product uint16
	Version uint16
}

// Mirrors Linux's uinput_user_dev structure on 64-bit Linux.
type uinputUserDev struct {
	Name       [80]byte
	ID         inputID
	EffectsMax int32
	AbsMax     [64]int32
	AbsMin     [64]int32
	AbsFuzz    [64]int32
	AbsFlat    [64]int32
}

// Mirrors Linux's input_event structure on 64-bit Linux.
type inputEvent struct {
	Time  syscall.Timeval
	Type  uint16
	Code  uint16
	Value int32
}

func ioctl(fd uintptr, request uintptr, arg uintptr) error {
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL, fd, request, arg,
	)
	if errno != 0 {
		return errno
	}
	return nil
}

func grabDevice(fd uintptr, grab bool) error {
	value := uintptr(0)
	if grab {
		value = 1
	}

	return ioctl(fd, eviocGrab, value)
}

func setBit(f *os.File, request uintptr, bit uintptr) error {
	return ioctl(f.Fd(), request, bit)
}

func createVirtualController() (*os.File, error) {
	f, err := os.OpenFile("/dev/uinput", os.O_WRONLY, 0)
	if err != nil {
		return nil, err
	}

	fail := func(err error) (*os.File, error) {
		f.Close()
		return nil, err
	}

	for _, typ := range []uintptr{evSyn, evKey, evAbs} {
		if err := setBit(f, uiSetEvBit, typ); err != nil {
			return fail(err)
		}
	}

	buttons := []uintptr{
		btnSouth, btnEast, btnWest, btnNorth,
		btnTL, btnTR, btnSelect, btnStart,
		btnMode, btnThumbL, btnThumbR,
	}

	for _, button := range buttons {
		if err := setBit(f, uiSetKeyBit, button); err != nil {
			return fail(err)
		}
	}

	axes := []uintptr{
		absX, absY, absZ, absRX, absRY, absRZ,
		absHat0X, absHat0Y,
	}

	for _, axis := range axes {
		if err := setBit(f, uiSetAbsBit, axis); err != nil {
			return fail(err)
		}
	}

	var dev uinputUserDev
	copy(dev.Name[:], "Xbox 360 Controller")

	dev.ID = inputID{
		BusType: busUSB,
		Vendor:  0x045e,
		Product: 0x028e,
		Version: 0x0114,
	}

	// Stick axes use the conventional signed range.
	for _, axis := range []int{absX, absY, absRX, absRY} {
		dev.AbsMin[axis] = -32768
		dev.AbsMax[axis] = 32767
	}

	// Triggers are 0 (released) to 255 (fully pressed).
	for _, axis := range []int{absZ, absRZ} {
		dev.AbsMin[axis] = 0
		dev.AbsMax[axis] = 255
	}

	// D-pad hat.
	for _, axis := range []int{absHat0X, absHat0Y} {
		dev.AbsMin[axis] = -1
		dev.AbsMax[axis] = 1
	}

	if err := binary.Write(f, binary.LittleEndian, dev); err != nil {
		return fail(err)
	}

	if err := ioctl(f.Fd(), uiDevCreate, 0); err != nil {
		return fail(err)
	}

	return f, nil
}

func writeEvent(f *os.File, typ, code uint16, value int32) error {
	now := time.Now()

	event := inputEvent{
		Time:  syscall.NsecToTimeval(now.UnixNano()),
		Type:  typ,
		Code:  code,
		Value: value,
	}

	return binary.Write(f, binary.LittleEndian, event)
}

// Converts a physical stick value (0..255) to a signed axis.
// The physical center is 128.
func scaleStick(value int32) int32 {
	if value < 0 {
		value = 0
	}
	if value > 255 {
		value = 255
	}

	if value < 128 {
		return (value - 128) * 32768 / 128
	}

	return (value - 128) * 32767 / 127
}

// Returns the translated type, code, value and whether to emit it.
func translate(event inputEvent) (uint16, uint16, int32, bool) {
	if event.Type == evKey {
		var code uint16

		switch event.Code {
		case physicalTriangle:
			code = btnWest
		case physicalCircle:
			code = btnEast
		case physicalX:
			code = btnSouth
		case physicalSquare:
			code = btnNorth
		case physicalL1:
			code = btnTL
		case physicalR1:
			code = btnTR
		case physicalSelect:
			code = btnSelect
		case physicalStart:
			code = btnStart
		case physicalL3:
			code = btnThumbL
		case physicalR3:
			code = btnThumbR
		case physicalL2:
			return evAbs, absZ, triggerValue(event.Value), true
		case physicalR2:
			return evAbs, absRZ, triggerValue(event.Value), true
		default:
			return 0, 0, 0, false
		}

		return evKey, code, event.Value, true
	}

	if event.Type == evAbs {
		switch event.Code {
		case 0x00: // Physical ABS_X: left stick X
			return evAbs, absX, scaleStick(event.Value), true
		case 0x01: // Physical ABS_Y: left stick Y
			return evAbs, absY, scaleStick(event.Value), true
		case 0x02: // Physical ABS_RZ: right stick Y
			return evAbs, absRY, scaleStick(event.Value), true
		case 0x05: // Physical ABS_Z: right stick X
			return evAbs, absRX, scaleStick(event.Value), true
		case 0x10: // D-pad horizontal
			return evAbs, absHat0X, event.Value, true
		case 0x11: // D-pad vertical
			return evAbs, absHat0Y, event.Value, true
		}
	}

	// Ignore MSC_SCAN, SYN_REPORT and unknown events.
	return 0, 0, 0, false
}

func triggerValue(value int32) int32 {
	if value == 0 {
		return 0
	}
	return 255
}

func main() {
	grab := flag.Bool("grab", false, "exclusively grab the physical controller")
	flag.Parse()

	if flag.NArg() != 1 {
		fmt.Printf("usage: %s [-grab] /dev/input/eventN\n", os.Args[0])
		os.Exit(1)
	}

	physical, err := os.Open(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "Open physical controller:", err)
		os.Exit(1)
	}
	defer physical.Close()

	virtual, err := createVirtualController()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Create virtual controller:", err)
		os.Exit(1)
	}
	defer func() {
		if err := ioctl(virtual.Fd(), uiDevDestroy, 0); err != nil {
			fmt.Fprintln(os.Stderr, "Destroy virtual controller:", err)
		}
		virtual.Close()
	}()

	if *grab {
		if err := grabDevice(physical.Fd(), true); err != nil {
			fmt.Fprintln(os.Stderr, "grab physical controller: ", err)
		}
		defer grabDevice(physical.Fd(), false)
	}

	fmt.Println("Remapper running.")
	fmt.Printf("Physical: %s\n", flag.Arg(0))
	fmt.Println("Virtual: Xbox 360 Controller")
	fmt.Println("Press Ctrl+C to stop.")

	for {
		var event inputEvent

		if err := binary.Read(physical, binary.LittleEndian, &event); err != nil {
			if err == io.EOF {
				return
			}
			fmt.Fprintln(os.Stderr, "Read physical event:", err)
			return
		}

		if event.Type == evSyn {
			if event.Code == synReport {
				if err := writeEvent(virtual, evSyn, synReport, 0); err != nil {
					fmt.Fprintln(os.Stderr, "Write sync:", err)
					return
				}
			}
			continue
		}

		typ, code, value, ok := translate(event)
		if !ok {
			continue
		}

		if err := writeEvent(virtual, typ, code, value); err != nil {
			fmt.Fprintln(os.Stderr, "Write virtual event:", err)
			return
		}
	}
}
