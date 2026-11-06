package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"softrains-servo-rpc/internal/config"
	"softrains-servo-rpc/internal/controller"
	"softrains-servo-rpc/internal/gpio"
	"softrains-servo-rpc/internal/rpcserver"
	"softrains-servo-rpc/internal/speech"
	"strings"
	"syscall"

	"softrains-servo-rpc/internal/rag"

	"github.com/sirupsen/logrus"
)

func main() {

	configPath := flag.String("config", "config.json", "Path to connector configuration file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel) // todo: get from config
	logger.SetFormatter(&logrus.TextFormatter{FullTimestamp: true})

	// initialize local RAG engine
	ragEngine, err := rag.NewEngine(cfg.RAG.IndexPath, logger)
	if err != nil {
		logger.Fatalf("failed to initialize RAG engine: %v", err)
	}
	defer func() {
		if err := ragEngine.Close(); err != nil {
			logger.Errorf("failed to close RAG engine: %v", err)
		}
	}()

	// Select the board backend from config.
	var board gpio.Board
	switch strings.ToLower(strings.TrimSpace(cfg.Board)) {
	case "raspberry":
		board, err = gpio.NewRaspberryPiBoard(logger)
	case "orange":
		board, err = gpio.NewOrangePiBoard(logger)
	default:
		logger.Fatalf("unsupported board type %q (supported: raspberry, orange)", cfg.Board)
	}
	if err != nil {
		logger.Fatalf("failed to initialize GPIO board: %v", err)
	}
	defer board.Close()

	// Start controller with the selected board and configuration.
	ctrl, err := controller.New(cfg, board, logger)
	if err != nil {
		logger.Fatalf("failed to initialize controller: %v", err)
	}

	// Initialize the speech recognition service first so the RPC server and voice activation share the same speech pathway.
	speechRecognizer, err := speech.NewSpeechRecognizer(board, cfg, logger, ragEngine)
	var sharedTalker *speech.Aawaz
	if err != nil {
		logger.Errorf("failed to initialize speech recognition: %v", err)
		sharedTalker = speech.NewAawaz(logger, cfg.SpeakCommand)
	} else {
		sharedTalker = speechRecognizer.Talker()
	}

	// Start the RPC server to listen for incoming gimbal control requests.
	srv, err := rpcserver.New(cfg, ctrl, logger, ragEngine, sharedTalker)
	if err != nil {
		logger.Fatalf("failed to initialize rpc server: %v", err)
	}
	go func() {
		if serveErr := srv.Serve(); serveErr != nil {
			logger.Infof("rpc server stopped: %v", serveErr)
		}
	}()
	logger.Infof("gimbal rpc listening on %s", cfg.ListenAddress)

	// start the speech recognition service in a separate goroutine
	if speechRecognizer != nil {
		go func() {
			err := speechRecognizer.StartSpeechRecognition()
			if err != nil {
				logger.Errorf("failed to start speech recognition: %v", err)
			}
		}()
	}

	// Wait for termination signal to gracefully shutdown the server.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	logger.Infoln("shutdown requested")
	_ = srv.Close()
}
