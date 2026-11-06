package main

import (
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"softrains-detector/pkg/softrainsvision"
	"softrains-detector/pkg/startup"

	"github.com/sirupsen/logrus"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: rpctester <imagefile> [rpc-endpoint] [ca-cert] [server-name]")
		os.Exit(1)
	}
	imgPath := os.Args[1]
	endpoint := "cam2hub.local:9443"
	caCert := "softrains.local.cert.pem"
	serverName := "softrains.local"
	if len(os.Args) > 2 {
		endpoint = os.Args[2]
	}
	if len(os.Args) > 3 {
		caCert = os.Args[3]
	}
	if len(os.Args) > 4 {
		serverName = os.Args[4]
	}

	file, err := os.Open(imgPath)
	if err != nil {
		fmt.Printf("Failed to open image: %v\n", err)
		os.Exit(1)
	}
	defer file.Close()
	img, _, err := image.Decode(file)
	if err != nil {
		fmt.Printf("Failed to decode image: %v\n", err)
		os.Exit(1)
	}

	aiConfig := startup.AIConfig{
		RPCEndpoint:         endpoint,
		ServerName:          serverName,
		CACert:              caCert,
		ConfidenceThreshold: 0.5,
		NMSThreshold:        0.4,
		DetectionFilters: startup.DetectionFiltersConfig{
			MinBoxWidth:    20,
			MinBoxHeight:   40,
			MinBoxArea:     400,
			MaxAreaRatio:   0.67,
			MaxWidthRatio:  0.9,
			MaxHeightRatio: 0.95,
		},
	}

	logger := logrus.New()
	detector, err := softrainsvision.NewHumanDetectorRPC(
		aiConfig,
		logger,
	)
	if err != nil {
		fmt.Printf("Failed to connect to detector: %v\n", err)
		os.Exit(1)
	}
	defer detector.Close()

	detections, err := detector.DetectHumans(img)
	if err != nil {
		fmt.Printf("Detection failed: %v\n", err)
		os.Exit(1)
	}

	if len(detections) == 0 {
		fmt.Println("No detections found.")
		return
	}

	fmt.Printf("%-10s %-10s %-10s %-10s %-10s\n", "Conf", "X", "Y", "W", "H")
	fmt.Println("------------------------------------------------------")
	for _, d := range detections {
		box := d.BoundingBox
		fmt.Printf("%-10.2f %-10d %-10d %-10d %-10d\n", d.Confidence, box.Min.X, box.Min.Y, box.Dx(), box.Dy())
	}
}
