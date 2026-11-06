package controller

import (
	"fmt"
	"math"
	"sync"
	"time"

	"softrains-servo-rpc/internal/config"

	"github.com/sirupsen/logrus"
)

type GPIO interface {
	PulseServo(pinNum int, pulseWidthMicros int, pulseCount int, period time.Duration) error
	SetDigital(pinNum int, on bool, activeHigh bool) error
}

type axisState struct {
	lastAngle     float64
	lastAppliedAt time.Time
}

type Controller struct {
	cfg       *config.Config
	gpio      GPIO
	mu        sync.Mutex
	axisState map[string]axisState
	laserOn   bool
	switches  map[int]bool
	log       *logrus.Logger
}

func New(cfg *config.Config, gpio GPIO, log *logrus.Logger) (*Controller, error) {
	c := &Controller{
		cfg:       cfg,
		gpio:      gpio,
		axisState: map[string]axisState{},
		switches:  map[int]bool{},
		laserOn:   cfg.Laser.DefaultOn,
		log:       log,
	}

	// Ensure all lights start OFF before the startup sequence
	if cfg.Laser.Enabled {
		if err := gpio.SetDigital(cfg.Laser.Pin, false, cfg.Laser.ActiveHigh); err != nil {
			return nil, fmt.Errorf("init laser state: %w", err)
		}
	}
	for _, sw := range cfg.Switches {
		c.switches[sw.ID] = sw.DefaultOn
		if sw.Enabled {
			if err := gpio.SetDigital(sw.Pin, false, sw.ActiveHigh); err != nil {
				return nil, fmt.Errorf("init switch %d (%s) state: %w", sw.ID, sw.Name, err)
			}
		}
	}

	// Startup sequence: cycle each light on and off one at a time
	cycledPins := map[int]bool{}
	for _, sw := range cfg.Switches {
		if !sw.Enabled {
			continue
		}
		log.Infof("startup sequence: cycling switch %d (%s) on pin %d", sw.ID, sw.Name, sw.Pin)
		if err := gpio.SetDigital(sw.Pin, true, sw.ActiveHigh); err != nil {
			return nil, fmt.Errorf("startup turn on switch %d (%s): %w", sw.ID, sw.Name, err)
		}
		time.Sleep(500 * time.Millisecond)
		if err := gpio.SetDigital(sw.Pin, false, sw.ActiveHigh); err != nil {
			return nil, fmt.Errorf("startup turn off switch %d (%s): %w", sw.ID, sw.Name, err)
		}
		time.Sleep(200 * time.Millisecond)
		if sw.DefaultOn {
			if err := gpio.SetDigital(sw.Pin, true, sw.ActiveHigh); err != nil {
				return nil, fmt.Errorf("restore default switch %d state: %w", sw.ID, err)
			}
		}
		cycledPins[sw.Pin] = true
	}

	if cfg.Laser.Enabled && !cycledPins[cfg.Laser.Pin] {
		log.Infof("startup sequence: cycling laser on pin %d", cfg.Laser.Pin)
		if err := gpio.SetDigital(cfg.Laser.Pin, true, cfg.Laser.ActiveHigh); err != nil {
			return nil, fmt.Errorf("startup turn on laser: %w", err)
		}
		time.Sleep(500 * time.Millisecond)
		if err := gpio.SetDigital(cfg.Laser.Pin, cfg.Laser.DefaultOn, cfg.Laser.ActiveHigh); err != nil {
			return nil, fmt.Errorf("set default laser state: %w", err)
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Startup sequence: full 0-180 sweep before settling at midpoint (Home)
	for _, axisEntry := range []struct {
		name string
		cfg  config.AxisConfig
	}{
		{name: "pan", cfg: cfg.Pan},
		{name: "tilt", cfg: cfg.Tilt},
		{name: "roll", cfg: cfg.Roll},
	} {
		axis := axisEntry.name
		axisCfg := axisEntry.cfg
		if !axisCfg.Enabled {
			continue
		}
		log.Infof("startup sequence: sweeping %s from %.1f to %.1f and settling at %.1f", axis, axisCfg.Min, axisCfg.Max, axisCfg.Home)
		if err := c.applyAxis(axis, axisCfg.Min, true); err != nil {
			return nil, fmt.Errorf("sweep min position for %s: %w", axis, err)
		}
		time.Sleep(300 * time.Millisecond)
		if err := c.applyAxis(axis, axisCfg.Max, true); err != nil {
			return nil, fmt.Errorf("sweep max position for %s: %w", axis, err)
		}
		time.Sleep(300 * time.Millisecond)
		if err := c.applyAxis(axis, axisCfg.Home, true); err != nil {
			return nil, fmt.Errorf("set home position for %s: %w", axis, err)
		}
	}

	return c, nil
}

func (c *Controller) SetAxis(axis string, position float64) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.log.Infof("setting axis %s to position %f", axis, position)
	return c.applyAxis(axis, position, false)
}

func (c *Controller) SetSwitch(id int, state bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	sw, ok := c.lookupSwitch(id)
	if !ok {
		return fmt.Errorf("switch %d is not configured", id)
	}
	if !sw.Enabled {
		return fmt.Errorf("switch %d is disabled", id)
	}
	if curr, ok := c.switches[id]; ok && curr == state {
		return nil
	}
	if err := c.gpio.SetDigital(sw.Pin, state, sw.ActiveHigh); err != nil {
		return err
	}
	c.switches[id] = state
	return nil
}

func (c *Controller) State() map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := map[string]any{
		"laser": c.laserOn,
	}
	for axis, state := range c.axisState {
		out[axis] = state.lastAngle
	}
	sw := map[int]bool{}
	for id, v := range c.switches {
		sw[id] = v
	}
	out["switches"] = sw
	return out
}

func (c *Controller) applyAxis(axis string, position float64, force bool) error {
	axisCfg, err := c.axisConfig(axis)
	if err != nil {
		return err
	}

	position = clamp(position, axisCfg.Min, axisCfg.Max)

	state := c.axisState[axis]
	minInterval := time.Duration(c.cfg.Resource.MinAxisCommandIntervalMs) * time.Millisecond
	if !force && !state.lastAppliedAt.IsZero() {
		if minInterval > 0 && time.Since(state.lastAppliedAt) < minInterval {
			if math.Abs(position-state.lastAngle) < c.cfg.Resource.MinAxisDeltaDegrees {
				return nil
			}
		}
	}
	c.log.Infof("applying to pin %d for axis %s with position %f", axisCfg.Pin, axis, position)
	pulse := angleToPulse(position, axisCfg.Min, axisCfg.Max, c.cfg.PWM.MinPulseWidth, c.cfg.PWM.MaxPulseWidth)
	period := time.Duration(c.cfg.PWM.PeriodMs) * time.Millisecond
	if err := c.gpio.PulseServo(axisCfg.Pin, pulse, c.cfg.PWM.PulseCount, period); err != nil {
		return err
	}
	c.axisState[axis] = axisState{lastAngle: position, lastAppliedAt: time.Now()}
	return nil
}

func (c *Controller) axisConfig(axis string) (config.AxisConfig, error) {
	switch axis {
	case "pan":
		return c.cfg.Pan, nil
	case "tilt":
		return c.cfg.Tilt, nil
	case "roll":
		return c.cfg.Roll, nil
	default:
		return config.AxisConfig{}, fmt.Errorf("unknown axis %s", axis)
	}
}

func (c *Controller) lookupSwitch(id int) (config.SwitchConfig, bool) {
	for _, sw := range c.cfg.Switches {
		if sw.ID == id {
			return sw, true
		}
	}
	return config.SwitchConfig{}, false
}

func angleToPulse(angle, min, max float64, minPulse, maxPulse int) int {
	if max <= min {
		return minPulse
	}
	ratio := (angle - min) / (max - min)
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	return int(math.Round(float64(minPulse) + ratio*float64(maxPulse-minPulse)))
}

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
