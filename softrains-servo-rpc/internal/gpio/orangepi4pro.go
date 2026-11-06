package gpio

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// OrangePiBoard implements the Board interface for the Orange Pi 4 Pro
type OrangePiBoard struct {
	mu          sync.Mutex
	opened      bool
	openHandles map[int]*os.File
	logger      *logrus.Logger
}

// NewOrangePiBoard initializes the board interface with a logrus logger
func NewOrangePiBoard(logger *logrus.Logger) (*OrangePiBoard, error) {
	return &OrangePiBoard{
		opened:      true,
		openHandles: make(map[int]*os.File),
		logger:      logger,
	}, nil
}

// ensureExported handles the system initialization files dynamically for a given pin with retry logic
func (b *OrangePiBoard) ensureExported(pinNum int) error {
	if b.openHandles[pinNum] != nil {
		return nil
	}

	pinDir := fmt.Sprintf("/sys/class/gpio/gpio%d", pinNum)
	valuePath := filepath.Join(pinDir, "value")

	// 1. Prefer `gpio export <pin> out` which uses wiringOP's setuid helper to export,
	// configure output direction, and set ownership on /sys/class/gpio/gpio<pin>/value.
	if out, err := exec.Command("gpio", "export", strconv.Itoa(pinNum), "out").CombinedOutput(); err != nil {
		b.logger.WithField("pin", pinNum).WithError(err).Debugf("gpio export fallback to sysfs (%s)", string(out))

		// Fallback: direct sysfs export if folder does not exist
		if _, statErr := os.Stat(pinDir); os.IsNotExist(statErr) {
			b.logger.WithField("pin", pinNum).Info("GPIO pin folder not found, attempting sysfs export")
			if writeErr := os.WriteFile("/sys/class/gpio/export", []byte(fmt.Sprintf("%d\n", pinNum)), 0644); writeErr != nil {
				b.logger.WithField("pin", pinNum).WithError(writeErr).Error("Failed to write to export file")
				return fmt.Errorf("failed to write to export for pin %d: %w", pinNum, writeErr)
			}
		}

		// Wait and verify that the kernel actually spawned the directory
		success := false
		for i := 0; i < 10; i++ {
			if _, statErr := os.Stat(pinDir); statErr == nil {
				success = true
				break
			}
			b.logger.WithField("pin", pinNum).Debugf("Waiting for sysfs directory path generation (attempt %d/10)", i+1)
			time.Sleep(50 * time.Millisecond)
		}
		if !success {
			b.logger.WithField("pin", pinNum).Error("Kernel sysfs directory population timed out")
			return fmt.Errorf("kernel failed to create directory structure for pin %d in time", pinNum)
		}

		directionPath := filepath.Join(pinDir, "direction")
		if dirErr := os.WriteFile(directionPath, []byte("out\n"), 0644); dirErr != nil {
			b.logger.WithField("pin", pinNum).WithError(dirErr).Error("Failed to force output direction configuration")
			return fmt.Errorf("failed to set direction for pin %d: %w", pinNum, dirErr)
		}
	}

	// 2. Cache the raw value file handle (Required for SetDigital and PulseServo precision)
	handle, err := os.OpenFile(valuePath, os.O_WRONLY, 0644)
	if err != nil {
		b.logger.WithField("pin", pinNum).WithError(err).Error("Failed to open pin descriptor value file stream")
		return fmt.Errorf("failed to open value path for pin %d: %w", pinNum, err)
	}
	b.openHandles[pinNum] = handle
	b.logger.WithField("pin", pinNum).Debug("Cached persistent file descriptor for precision timing blocks")

	return nil
}

// SetDigital toggles a pin safely after verification
func (b *OrangePiBoard) SetDigital(pinNum int, on bool, activeHigh bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.opened {
		return fmt.Errorf("gpio board is closed")
	}

	// 1. Force the file initialization and verification loops first
	if err := b.ensureExported(pinNum); err != nil {
		return fmt.Errorf("initialization failed inside SetDigital: %w", err)
	}

	// 2. Determine physical bit state based on inversion settings
	valueStr := "0"
	if on == activeHigh {
		valueStr = "1"
	}

	b.logger.WithFields(logrus.Fields{
		"pin":   pinNum,
		"value": valueStr,
	}).Debug("Executing SetDigital sysfs write")

	if _, err := b.openHandles[pinNum].WriteString(valueStr + "\n"); err != nil {
		b.logger.WithField("pin", pinNum).WithError(err).Error("Failed to write digital pin state")
		return fmt.Errorf("error setting digital state on pin %d: %w", pinNum, err)
	}

	return nil
}

// PulseServo generates high/low timings precisely on a targeting pin channel
func (b *OrangePiBoard) PulseServo(pinNum int, pulseWidthMicros int, pulseCount int, period time.Duration) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.opened {
		return fmt.Errorf("gpio board is closed")
	}

	// 1. Ensure the pin is initialized and handle is cached
	if err := b.ensureExported(pinNum); err != nil {
		return fmt.Errorf("initialization failed inside PulseServo: %w", err)
	}
	valueFile := b.openHandles[pinNum]

	// 2. Parse precision metrics
	highDuration := time.Duration(pulseWidthMicros) * time.Microsecond
	if highDuration <= 0 || highDuration >= period {
		b.logger.WithFields(logrus.Fields{
			"pulseWidthMicros": pulseWidthMicros,
			"period":           period,
		}).Error("Aborting pulse track: Out-of-bounds configuration metrics received")
		return fmt.Errorf("invalid pulse width %v for period window %v", highDuration, period)
	}
	lowDuration := period - highDuration

	b.logger.WithFields(logrus.Fields{
		"pin":        pinNum,
		"pulseWidth": highDuration,
		"pulseCount": pulseCount,
	}).Debug("Beginning native software PWM bit-bang transaction window")

	// 3. Lock OS thread and spin-wait during the short HIGH pulse to avoid scheduler jitter
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	for i := 0; i < pulseCount; i++ {
		pulseStart := time.Now()
		_, _ = valueFile.WriteString("1\n")
		for time.Since(pulseStart) < highDuration {
		}

		_, _ = valueFile.WriteString("0\n")
		time.Sleep(lowDuration)
	}

	return nil
}

// Close releases the file stream trackers safely
func (b *OrangePiBoard) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.opened = false
	b.logger.Info("Closing GPIO Board interface wrapper and flushing handle tables")

	for pin, handle := range b.openHandles {
		if handle != nil {
			_ = handle.Close()
		}
		// Unexport pin to leave the kernel in a clean baseline state
		b.logger.WithField("pin", pin).Debug("Unexporting hardware channel line allocation hook")
		if err := exec.Command("gpio", "unexport", strconv.Itoa(pin)).Run(); err != nil {
			_ = os.WriteFile("/sys/class/gpio/unexport", []byte(fmt.Sprintf("%d\n", pin)), 0644)
		}
	}
	b.openHandles = make(map[int]*os.File)
}

