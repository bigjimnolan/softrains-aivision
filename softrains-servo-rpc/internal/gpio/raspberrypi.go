package gpio

import (
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	rpio "github.com/stianeikeland/go-rpio/v4"
)

// RaspberryPiBoard represents the GPIO interface for Raspberry Pi
type RaspberryPiBoard struct {
	mu      sync.Mutex
	opened  bool
	pinMode map[int]struct{}
	log     *logrus.Logger
}

func NewRaspberryPiBoard(log *logrus.Logger) (*RaspberryPiBoard, error) {
	if err := rpio.Open(); err != nil {
		return nil, fmt.Errorf("open rpio: %w", err)
	}
	return &RaspberryPiBoard{log: log, opened: true, pinMode: map[int]struct{}{}}, nil
}

func (b *RaspberryPiBoard) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.opened {
		rpio.Close()
		b.opened = false
	}
}

func (b *RaspberryPiBoard) configureOutput(pinNum int) (rpio.Pin, error) {
	if pinNum <= 0 {
		return 0, fmt.Errorf("invalid pin: %d", pinNum)
	}
	pin := rpio.Pin(pinNum)
	if _, ok := b.pinMode[pinNum]; !ok {
		pin.Output()
		b.pinMode[pinNum] = struct{}{}
	}
	return pin, nil
}

func (b *RaspberryPiBoard) SetDigital(pinNum int, on bool, activeHigh bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.opened {
		return fmt.Errorf("gpio board is closed")
	}
	pin, err := b.configureOutput(pinNum)
	if err != nil {
		return err
	}
	if on == activeHigh {
		pin.High()
	} else {
		pin.Low()
	}
	return nil
}

func (b *RaspberryPiBoard) PulseServo(pinNum int, pulseWidthMicros int, pulseCount int, period time.Duration) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.opened {
		return fmt.Errorf("gpio board is closed")
	}
	pin, err := b.configureOutput(pinNum)
	if err != nil {
		return err
	}
	pulseDur := time.Duration(pulseWidthMicros) * time.Microsecond
	if pulseDur <= 0 || pulseDur >= period {
		return fmt.Errorf("invalid pulse width %d for period %s", pulseWidthMicros, period)
	}
	lowDur := period - pulseDur
	for i := 0; i < pulseCount; i++ {
		pin.High()
		time.Sleep(pulseDur)
		pin.Low()
		time.Sleep(lowDur)
	}
	return nil
}
