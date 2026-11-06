package main

import (
	"flag"
	"fmt"
	"log"
	"softrains-servo-rpc/internal/config"
	"softrains-servo-rpc/internal/controller"
	"softrains-servo-rpc/internal/gpio"
	"strings"

	"github.com/sirupsen/logrus"
)

func main() {
	configPath := flag.String("config", "config.json", "Path to connector configuration file")
	axis := flag.String("axis", "pan", "Axis to drive: pan, tilt, or roll")
	position := flag.Float64("position", 90, "Target axis position in degrees")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)
	logger.SetFormatter(&logrus.TextFormatter{FullTimestamp: true})

	var board gpio.Board
	switch strings.ToLower(strings.TrimSpace(cfg.Board)) {
	case "raspberry":
		board, err = gpio.NewRaspberryPiBoard()
	case "orange":
		board, err = gpio.NewOrangePiBoard(logger)
	default:
		log.Fatalf("unsupported board type %q (supported: raspberry, orange)", cfg.Board)
	}
	if err != nil {
		log.Fatalf("failed to initialize GPIO board: %v", err)
	}
	defer board.Close()

	ctrl, err := controller.New(cfg, board, logger)
	if err != nil {
		log.Fatalf("failed to initialize controller: %v", err)
	}

	requestedAxis := strings.ToLower(strings.TrimSpace(*axis))
	fmt.Printf("Driving axis %s to %.2f degrees using board %s and config %s...\n", requestedAxis, *position, cfg.Board, *configPath)

	if err := ctrl.SetAxis(requestedAxis, *position); err != nil {
		log.Fatalf("failed to drive axis %s to %.2f: %v", requestedAxis, *position, err)
	}

	fmt.Printf("Axis %s command applied. Current state: %#v\n", requestedAxis, ctrl.State())
}
