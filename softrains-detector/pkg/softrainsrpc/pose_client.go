package softrainsrpc

import (
	"context"
	"fmt"
	"time"
)

type PoseClient struct {
	line *Client
}

func NewPoseClient(endpoint, serverName, caCertPath string, timeout time.Duration) (*PoseClient, error) {
	line, err := NewClientFromCA(endpoint, serverName, caCertPath, timeout, 256*1024)
	if err != nil {
		return nil, err
	}
	return &PoseClient{line: line}, nil
}

func (c *PoseClient) DetectRGB24(ctx context.Context, width, height int, rgbBytes []byte, includePose bool, requestID string) (Envelope, error) {
	expectedSize := width * height * 3
	if len(rgbBytes) != expectedSize {
		return Envelope{}, fmt.Errorf("invalid rgb payload size: expected %d, got %d", expectedSize, len(rgbBytes))
	}

	header := map[string]any{
		// Legacy fields expected by the current pose connector.
		"width":        width,
		"height":       height,
		"format":       "rgb24",
		"include_pose": includePose,
		// Strict envelope fields for schema symmetry.
		"schema":     SchemaV1,
		"service":    "pose",
		"command":    "detect_humans",
		"request_id": requestID,
	}

	raw, err := c.line.Call(ctx, header, rgbBytes)
	if err != nil {
		return Envelope{}, err
	}

	env, err := NormalizeResponse(raw, "pose", "detect_humans", requestID)
	if err != nil {
		return Envelope{}, err
	}
	return env, nil
}
