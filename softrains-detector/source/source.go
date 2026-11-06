package source

import (
	"softrains-detector/pkg/startup"

	"gocv.io/x/gocv"
)

type CaptureSource interface {
	Read(frame *gocv.Mat) bool
	Close() error
	Describe() string
}

func OpenCaptureSource(connection startup.ConnectionConfig) (CaptureSource, error) {
	switch connection.Source {
	case "rtsp":
		return newRTSPSource(connection.Camera.URL)
	case "snapshot":
		return newSnapshotSource(connection.Camera.URL)
	case "usb":
		return newUSBSource(connection.Camera.Index, connection.Camera.Width, connection.Camera.Height)
	case "zerocam":
		return newZerocamSource(connection.Camera.URL, connection.Camera.Quality)
	default:
		return nil, ErrUnsupportedSource
	}
}
