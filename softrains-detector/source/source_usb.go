package source

import (
	"fmt"

	"gocv.io/x/gocv"
)

type usbSource struct {
	index   int
	width   int
	height  int
	capture *gocv.VideoCapture
}

func newUSBSource(index, width, height int) (*usbSource, error) {
	cam, err := gocv.OpenVideoCapture(index)
	if err != nil {
		return nil, fmt.Errorf("failed to open USB camera index %d: %w", index, err)
	}

	// Best effort property requests; some webcams may ignore one or more values.
	if width > 0 {
		cam.Set(gocv.VideoCaptureFrameWidth, float64(width))
	}
	if height > 0 {
		cam.Set(gocv.VideoCaptureFrameHeight, float64(height))
	}

	return &usbSource{
		index:   index,
		width:   width,
		height:  height,
		capture: cam,
	}, nil
}

func (s *usbSource) Read(frame *gocv.Mat) bool {
	return s.capture.Read(frame)
}

func (s *usbSource) Close() error {
	if s.capture != nil {
		s.capture.Close()
		s.capture = nil
	}
	return nil
}

func (s *usbSource) Describe() string {
	return fmt.Sprintf("usb:index=%d@%dx%d/%dfps", s.index, s.width, s.height)
}
