package sensorbridge

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
)

type Reading struct {
	Name      string
	Value     float64
	Timestamp time.Time
}

type SensorReader interface {
	Read(ctx context.Context) (Reading, error)
}

type DeviceActivator interface {
	SetActive(ctx context.Context, active bool) error
}

type Rule struct {
	ActivateAbove float64
}

type Service struct {
	sensor       SensorReader
	device       DeviceActivator
	rule         Rule
	logger       *logrus.Logger
	lastDecision *bool
}

func New(sensor SensorReader, device DeviceActivator, rule Rule, logger *logrus.Logger) *Service {
	if logger == nil {
		logger = logrus.New()
	}
	return &Service{
		sensor: sensor,
		device: device,
		rule:   rule,
		logger: logger,
	}
}

func (s *Service) RunOnce(ctx context.Context) error {
	reading, err := s.sensor.Read(ctx)
	if err != nil {
		return fmt.Errorf("read sensor: %w", err)
	}

	shouldActivate := reading.Value >= s.rule.ActivateAbove
	if s.lastDecision != nil && *s.lastDecision == shouldActivate {
		s.logger.WithFields(logrus.Fields{
			"sensor": reading.Name,
			"value":  reading.Value,
			"state":  shouldActivate,
		}).Debug("decision unchanged")
		return nil
	}

	if err := s.device.SetActive(ctx, shouldActivate); err != nil {
		return fmt.Errorf("apply device action: %w", err)
	}

	s.lastDecision = new(bool)
	*s.lastDecision = shouldActivate

	s.logger.WithFields(logrus.Fields{
		"sensor": reading.Name,
		"value":  reading.Value,
		"state":  shouldActivate,
	}).Info("applied sensor rule")

	return nil
}

func (s *Service) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = 1 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		if err := s.RunOnce(ctx); err != nil {
			s.logger.WithError(err).Warn("sensor cycle failed")
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
