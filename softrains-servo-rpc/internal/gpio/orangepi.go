package gpio

/*
#cgo LDFLAGS: -lwiringPi
#include <wiringPi.h>
*/
import "C"
import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	orangePiPWMBaseClockHz = 24_000_000
	orangePiPWMRange       = 2000
)

type orangePiPWMState struct {
	periodMicros int
	divider      int
	value        int
}

// OrangePi2WBoard drives Orange Pi pins through the gpio CLI and uses hardware PWM for servos.
type OrangePi2WBoard struct {
	mu          sync.Mutex
	opened      bool
	log         *logrus.Logger
	digitalPins map[int]struct{}
	pwmPins     map[int]orangePiPWMState
	modeFlag    string
	modeLabel   string
	servoFlag   string
	servoLabel  string
}

// NewOrangePi2WBoard creates a new Orange Pi controller backed by the gpio CLI.
func NewOrangePi2WBoard(log *logrus.Logger) (*OrangePi2WBoard, error) {
	if _, err := exec.LookPath("gpio"); err != nil {
		return nil, fmt.Errorf("gpio command not found in PATH: %w", err)
	}

	return &OrangePi2WBoard{
		opened:      true,
		log:         log,
		digitalPins: map[int]struct{}{},
		pwmPins:     map[int]orangePiPWMState{},
		modeLabel:   "gpio",
		servoLabel:  "gpio",
	}, nil

}

func (b *OrangePi2WBoard) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.opened {
		return
	}

	for pin := range b.pwmPins {
		if err := b.runServoGPIO("pwm", strconv.Itoa(pin), "0"); err != nil && b.log != nil {
			b.log.Warnf("failed to disable PWM on pin %d during close: %v", pin, err)
		}
	}

	b.opened = false
	b.digitalPins = map[int]struct{}{}
	b.pwmPins = map[int]orangePiPWMState{}
}

func (b *OrangePi2WBoard) SetDigital(pinNum int, on bool, activeHigh bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.opened {
		return fmt.Errorf("gpio board is closed")
	}
	if pinNum <= 0 {
		return fmt.Errorf("invalid pin %d", pinNum)
	}

	if err := b.ensureDigitalMode(pinNum); err != nil {
		return err
	}

	value := "0"
	if on == activeHigh {
		value = "1"
	}

	if err := b.runDigitalGPIO("write", strconv.Itoa(pinNum), value); err != nil {
		return fmt.Errorf("set digital pin %d=%s: %w", pinNum, value, err)
	}
	if b.log != nil {
		b.log.Debugf("orange digital pin=%d mode=%s value=%s", pinNum, b.modeLabel, value)
	}

	return nil

}

func (b *OrangePi2WBoard) PulseServo(pinNum int, pulseWidthMicros int, pulseCount int, period time.Duration) error {
	b.log.Infof("orange pulse pin=%d pulseWidth=%dus pulseCount=%d period=%s", pinNum, pulseWidthMicros, pulseCount, period)

	if int(C.wiringPiSetup()) == -1 {
		return fmt.Errorf("failed to initialize wiringPi setup")
	}

	C.pinMode(C.int(pinNum), C.PWM_OUTPUT)

	C.pwmSetMode(C.int(pinNum), C.PWM_MODE_MS)
	C.pwmSetClock(C.int(pinNum), C.int(192))
	C.pwmSetRange(C.int(pinNum), C.uint(2000))
	C.pwmWrite(C.int(pinNum), C.int(pulseWidthMicros/10))

	return nil
}

func (b *OrangePi2WBoard) ensureDigitalMode(pinNum int) error {
	if _, ok := b.digitalPins[pinNum]; ok {
		return nil
	}

	if err := b.runDigitalGPIO("mode", strconv.Itoa(pinNum), "out"); err != nil {
		return fmt.Errorf("set digital mode on pin %d: %w", pinNum, err)
	}
	if err := b.verifyMode(pinNum, b.modeFlag, "out"); err != nil {
		return fmt.Errorf("verify digital mode on pin %d: %w", pinNum, err)
	}
	b.digitalPins[pinNum] = struct{}{}
	return nil
}

func (b *OrangePi2WBoard) ensurePWMMode(pinNum int, periodMicros int, divider int) error {
	state, ok := b.pwmPins[pinNum]
	if ok && state.periodMicros == periodMicros && state.divider == divider {
		return nil
	}

	if err := b.runServoGPIO("mode", strconv.Itoa(pinNum), "pwm"); err != nil {
		return fmt.Errorf("set PWM mode on pin %d using %s numbering: %w", pinNum, b.servoLabel, err)
	}
	if err := b.verifyMode(pinNum, b.servoFlag, "pwm"); err != nil {
		return fmt.Errorf("verify PWM mode on pin %d using %s numbering: %w", pinNum, b.servoLabel, err)
	}
	if err := b.runServoGPIO("pwmr", strconv.Itoa(pinNum), strconv.Itoa(orangePiPWMRange)); err != nil {
		return fmt.Errorf("set PWM range on pin %d: %w", pinNum, err)
	}
	if err := b.runServoGPIO("pwmc", strconv.Itoa(pinNum), strconv.Itoa(divider)); err != nil {
		return fmt.Errorf("set PWM clock divider on pin %d: %w", pinNum, err)
	}

	if b.log != nil {
		b.log.Infof("configured orange pwm pin=%d servoMode=%s range=%d divider=%d period=%dus", pinNum, b.servoLabel, orangePiPWMRange, divider, periodMicros)
	}

	b.pwmPins[pinNum] = orangePiPWMState{periodMicros: periodMicros, divider: divider}
	return nil
}

func (b *OrangePi2WBoard) runServoGPIO(args ...string) error {
	fullArgs := make([]string, 0, len(args)+1)
	if b.servoFlag != "" {
		fullArgs = append(fullArgs, b.servoFlag)
	}
	fullArgs = append(fullArgs, args...)
	return b.runGPIO(fullArgs...)
}

func (b *OrangePi2WBoard) runDigitalGPIO(args ...string) error {
	fullArgs := make([]string, 0, len(args)+1)
	if b.modeFlag != "" {
		fullArgs = append(fullArgs, b.modeFlag)
	}
	fullArgs = append(fullArgs, args...)
	return b.runGPIO(fullArgs...)
}

func (b *OrangePi2WBoard) runGPIO(args ...string) error {
	output, err := runOrangePiGPIO(args...)
	if b.log != nil && strings.TrimSpace(output) != "" {
		b.log.Debugf("gpio %s => %s", strings.Join(args, " "), strings.TrimSpace(output))
	}
	return err
}

func (b *OrangePi2WBoard) verifyMode(pinNum int, flag string, expected string) error {
	args := make([]string, 0, 3)
	if flag != "" {
		args = append(args, flag)
	}
	args = append(args, "qmode", strconv.Itoa(pinNum))

	output, err := runOrangePiGPIO(args...)
	if err != nil {
		if isBrokenQMode(err) {
			mode, readallErr := b.verifyModeWithReadall(pinNum)
			if readallErr != nil {
				return fmt.Errorf("qmode failed and readall fallback failed for pin %d: %w", pinNum, readallErr)
			}
			if !strings.Contains(strings.ToLower(mode), strings.ToLower(expected)) {
				return fmt.Errorf("expected mode %q, got %q via readall fallback", expected, mode)
			}
			if b.log != nil {
				b.log.Warnf("gpio qmode crashed for pin %d; verified mode=%s via readall fallback", pinNum, mode)
			}
			return nil
		}
		return err
	}
	mode := strings.ToLower(strings.TrimSpace(output))
	if b.log != nil {
		b.log.Debugf("gpio %s => %s", strings.Join(args, " "), mode)
	}
	if !strings.Contains(mode, strings.ToLower(expected)) {
		return fmt.Errorf("expected mode %q, got %q", expected, strings.TrimSpace(output))
	}
	return nil
}

func (b *OrangePi2WBoard) verifyModeWithReadall(pinNum int) (string, error) {
	output, err := runOrangePiGPIO("readall")
	if err != nil {
		return "", err
	}
	mode, err := modeFromReadall(output, pinNum)
	if err != nil {
		return "", err
	}
	if b.log != nil {
		b.log.Debugf("gpio readall => pin %d mode %s", pinNum, mode)
	}
	return mode, nil
}

func modeFromReadall(output string, pinNum int) (string, error) {
	target := strconv.Itoa(pinNum)
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, "|") || !strings.Contains(line, target) {
			continue
		}

		fields := strings.Split(line, "|")
		for i := range fields {
			fields[i] = strings.TrimSpace(fields[i])
		}

		// OrangePi readall table columns are:
		// | GPIO | wPi | Name | Mode | ... || ... | Mode | Name | wPi | GPIO |
		// We verify by wPi because digital commands use wiringPi numbering.
		if len(fields) >= 5 && fields[2] == target {
			return fields[4], nil
		}

		// Readall includes two sides of the header. Resolve right-side wPi/mode
		// from the end so this stays stable even if spacing varies.
		if len(fields) >= 5 {
			rightWPIIdx := len(fields) - 3
			rightModeIdx := len(fields) - 5
			if rightWPIIdx >= 0 && rightModeIdx >= 0 && fields[rightWPIIdx] == target {
				return fields[rightModeIdx], nil
			}
		}
	}

	return "", fmt.Errorf("wPi pin %d not found in gpio readall output", pinNum)
}

func runOrangePiGPIO(args ...string) (string, error) {
	cmd := exec.Command("gpio", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed == "" {
			return "", fmt.Errorf("gpio %s: %w", strings.Join(args, " "), err)
		}
		return string(output), fmt.Errorf("gpio %s: %w: %s", strings.Join(args, " "), err, trimmed)
	}
	return string(output), nil
}

func isBrokenQMode(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "qmode") && strings.Contains(msg, "segmentation fault")
}
