package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"softrains-detector/pkg/imageprocessing"
	"softrains-detector/pkg/softrainsvision"
	"softrains-detector/pkg/startup"

	"github.com/sirupsen/logrus"
)

type SoftrainsCamService struct {
	appConfig      *startup.AppConfig
	startupProfile *startup.StartupProfile
	logger         *logrus.Logger
	detector       *imageprocessing.DetectionOnlyService
	actioneer      *softrainsvision.Actioneer
	name           string
	actioneerInput chan softrainsvision.TrackerSnapshot
}

func (scs *SoftrainsCamService) loadConfig(args []string) error {
	var err error
	scs.logger = logrus.New()
	scs.logger.SetLevel(logrus.DebugLevel)
	scs.logger.SetFormatter(&logrus.TextFormatter{FullTimestamp: true})

	configPath, found := extractStringFlag(args, "config")
	if !found {
		configPath = "config/config.json"
	}

	scs.appConfig, err = startup.LoadAppConfig(configPath)
	if err != nil {
		return err
	}

	startupProfile, found := extractStringFlag(args, "profile")
	if !found {
		startupProfile = "head-tracker"
	}
	interMed := scs.getStartupProfile(startupProfile)

	connection, err := scs.getConnection(interMed.BotSetup.ConnectionName)
	if err != nil {
		return err
	}
	interMed.BotSetup.Connection = connection

	aiConfig, err := scs.getAIConfig(interMed.BotSetup.AIName)
	if err != nil {
		return err
	}
	interMed.BotSetup.AI = aiConfig

	scs.startupProfile = &interMed
	if scs.startupProfile == nil {
		return fmt.Errorf("startup profile not found: %s", startupProfile)
	}

	scs.name = "softrains"
	return nil
}

func (scs *SoftrainsCamService) getStartupProfile(name string) startup.StartupProfile {
	for _, profile := range scs.appConfig.StartupProfiles {
		if profile.Name == name {
			return profile
		}
	}
	return startup.StartupProfile{}
}

func (scs *SoftrainsCamService) getConnection(name string) (startup.ConnectionConfig, error) {
	for _, conn := range scs.appConfig.Connections {
		if conn.Name == name {
			return conn, nil
		}
	}
	return startup.ConnectionConfig{}, fmt.Errorf("connection not found: %s", name)
}

func (scs *SoftrainsCamService) getAIConfig(name string) (startup.AIConfig, error) {
	for _, ai := range scs.appConfig.AI {
		if ai.Name == name {
			return ai, nil
		}
	}
	return startup.AIConfig{}, fmt.Errorf("AI config not found: %s", name)
}

func (scs *SoftrainsCamService) startActioneer() {
	// Initialize tracker (association/publishing) and actioneer (movement/laser control)
	interMed := softrainsvision.NewActioneer(*scs.startupProfile, scs.logger)
	scs.actioneer = interMed

	go func() {
		scs.actioneer.Start()
	}()
}

func (scs *SoftrainsCamService) startDetector() {
	if *scs.startupProfile.Detector.Enabled == true {
		snapshotURL := ""
		if scs.startupProfile.BotSetup.Connection.Camera != nil {
			snapshotURL = scs.startupProfile.BotSetup.Connection.Control.URL
		}

		detectorSvc, err := imageprocessing.NewDetectionOnlyService(
			*scs.startupProfile,
			scs.logger,
			scs.actioneer.GetInputChannel(),
			scs.actioneer.GetViewerChannel(),
			scs.actioneer,
			snapshotURL,
			120*time.Millisecond,
			3*time.Second,
		)
		if err != nil {
			scs.logger.WithError(err).Fatal("failed to setup detection-only service")
		}
		if *scs.startupProfile.Detector.Enabled == true {
			// call up viewer.ShowFrames
			go func() {
				scs.logger.Info("Starting viewer to show frames")
				err := scs.detector.ShowFrames()
				if err != nil {
					scs.logger.WithError(err).Fatal("failed to show frames")
				}
			}()
		}

		scs.detector = detectorSvc
		err = scs.detector.Start(context.Background())
		if err != nil {
			scs.logger.Infof("Detection-only error: %v\n", err)
			os.Exit(1)
		}
	}
}

func main() {
	scs := SoftrainsCamService{}
	err := scs.loadConfig(os.Args[1:])
	if err != nil {
		scs.logger.Infof("Failed to load config: %v\n", err)
		os.Exit(1)
	}
	scs.startActioneer()
	scs.startDetector()
	for {
	}
}

// Helper for extracting --profile and --config
func extractStringFlag(args []string, name string) (string, bool) {
	longName := "--" + name
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == longName {
			if i+1 >= len(args) {
				return "", true
			}
			return args[i+1], true
		}
		if strings.HasPrefix(arg, longName+"=") {
			return strings.TrimPrefix(arg, longName+"="), true
		}
	}
	return "", false
}
