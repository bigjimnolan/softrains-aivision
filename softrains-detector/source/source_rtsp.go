package source

import (
	"fmt"

	"gocv.io/x/gocv"
)

type rtspSource struct {
	url     string
	capture *gocv.VideoCapture
}

func newRTSPSource(url string) (*rtspSource, error) {
	stream, err := gocv.VideoCaptureFile(url)
	if err != nil {
		return nil, fmt.Errorf("failed to open RTSP stream: %w", err)
	}
	stream.Set(gocv.VideoCaptureBufferSize, 1)
	return &rtspSource{
		url:     url,
		capture: stream,
	}, nil
}

func (s *rtspSource) Read(frame *gocv.Mat) bool {
	// Drop a few queued packets so the viewer favors the newest frame instead of
	// replaying buffered video after detection/render stalls.
	s.capture.Grab(2)
	return s.capture.Read(frame)
}

func (s *rtspSource) Close() error {
	if s.capture != nil {
		s.capture.Close()
		s.capture = nil
	}
	return nil
}

func (s *rtspSource) Describe() string {
	return "rtsp:" + s.url
}
