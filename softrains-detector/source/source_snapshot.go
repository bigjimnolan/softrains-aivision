package source

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"gocv.io/x/gocv"
)

type snapshotSource struct {
	url        string
	httpClient *http.Client
}

func newSnapshotSource(url string) (*snapshotSource, error) {
	if url == "" {
		return nil, fmt.Errorf("snapshot source requires a URL")
	}

	client := &http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{
			DisableKeepAlives:   false,
			MaxIdleConns:        4,
			MaxIdleConnsPerHost: 4,
			IdleConnTimeout:     30 * time.Second,
		},
	}

	return &snapshotSource{
		url:        url,
		httpClient: client,
	}, nil
}

func (s *snapshotSource) Read(frame *gocv.Mat) bool {
	req, err := http.NewRequest(http.MethodGet, s.url, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Pragma", "no-cache")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false
	}

	jpegBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return false
	}

	buf, err := gocv.IMDecode(jpegBytes, gocv.IMReadColor)
	if err != nil || buf.Empty() {
		buf.Close()
		return false
	}

	buf.CopyTo(frame)
	buf.Close()
	return true
}

func (s *snapshotSource) Close() error {
	if transport, ok := s.httpClient.Transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}
	return nil
}

func (s *snapshotSource) Describe() string {
	return "snapshot:" + s.url
}
