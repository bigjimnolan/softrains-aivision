package gpio

import "time"

type Board interface {
	SetDigital(pinNum int, on bool, activeHigh bool) error
	PulseServo(pinNum int, pulseWidthMicros int, pulseCount int, period time.Duration) error
	Close()
}
