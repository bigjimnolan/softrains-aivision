package softrainsrpc

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"
)

type RelayClient struct {
	line *Client
}

type RelayState struct {
	Relay int    `json:"relay"`
	State string `json:"state"`
}

type RelayAllStates struct {
	States map[string]string `json:"states"`
	Count  int               `json:"count"`
}

func NewRelayClient(endpoint, serverName, caCertPath string, timeout time.Duration) (*RelayClient, error) {
	line, err := NewClientFromCA(endpoint, serverName, caCertPath, timeout, 64*1024)
	if err != nil {
		return nil, err
	}
	return &RelayClient{line: line}, nil
}

func NewRelayClientMTLS(endpoint, serverName, caCertPath, clientCertPath, clientKeyPath string, timeout time.Duration) (*RelayClient, error) {
	line, err := NewClientFromCAMTLS(endpoint, serverName, caCertPath, clientCertPath, clientKeyPath, timeout, 64*1024)
	if err != nil {
		return nil, err
	}
	return &RelayClient{line: line}, nil
}

func NewRelayClientWithTLSConfig(endpoint string, tlsConfig *tls.Config, timeout time.Duration) (*RelayClient, error) {
	line, err := NewClient(endpoint, tlsConfig, timeout, 64*1024)
	if err != nil {
		return nil, err
	}
	return &RelayClient{line: line}, nil
}

func (c *RelayClient) Ping(ctx context.Context, requestID string) (Envelope, error) {
	req := NewRequest("relay", "ping", requestID, 0, nil)
	return c.callEnvelope(ctx, req)
}

func (c *RelayClient) SetRelay(ctx context.Context, relay int, state string, requestID string) (Envelope, error) {
	req := NewRequest("relay", "set_relay", requestID, 0, map[string]any{
		"relay": relay,
		"state": state,
	})
	return c.callEnvelope(ctx, req)
}

func (c *RelayClient) GetRelay(ctx context.Context, relay int, requestID string) (RelayState, Envelope, error) {
	req := NewRequest("relay", "get_relay", requestID, 0, map[string]any{"relay": relay})
	env, err := c.callEnvelope(ctx, req)
	if err != nil {
		return RelayState{}, env, err
	}
	if !env.Success {
		return RelayState{}, env, fmt.Errorf("relay get failed: %s", env.Error)
	}
	var result RelayState
	if err := env.DecodeResult(&result); err != nil {
		return RelayState{}, env, err
	}
	return result, env, nil
}

func (c *RelayClient) GetAllStates(ctx context.Context, requestID string) (RelayAllStates, Envelope, error) {
	req := NewRequest("relay", "get_all_relays", requestID, 0, nil)
	env, err := c.callEnvelope(ctx, req)
	if err != nil {
		return RelayAllStates{}, env, err
	}
	if !env.Success {
		return RelayAllStates{}, env, fmt.Errorf("relay list failed: %s", env.Error)
	}
	var result RelayAllStates
	if err := env.DecodeResult(&result); err != nil {
		return RelayAllStates{}, env, err
	}
	return result, env, nil
}

func (c *RelayClient) callEnvelope(ctx context.Context, req Request) (Envelope, error) {
	header := map[string]any{
		"schema":     req.Schema,
		"service":    req.Service,
		"command":    req.Command,
		"request_id": req.RequestID,
		"params":     req.Params,
	}

	raw, err := c.line.Call(ctx, header, nil)
	if err != nil {
		return Envelope{}, err
	}

	env, err := NormalizeResponse(raw, "relay", req.Command, req.RequestID)
	if err != nil {
		return Envelope{}, err
	}
	return env, nil
}
