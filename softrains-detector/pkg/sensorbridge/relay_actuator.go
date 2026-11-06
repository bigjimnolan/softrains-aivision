package sensorbridge

import (
	"context"
	"fmt"
	"time"

	"softrains-detector/pkg/softrainsrpc"
)

type RelayActuator struct {
	Client *softrainsrpc.RelayClient
	Relay  int
}

func NewRelayActuator(endpoint, serverName, caCertPath string, timeout time.Duration, relay int) (*RelayActuator, error) {
	client, err := softrainsrpc.NewRelayClient(endpoint, serverName, caCertPath, timeout)
	if err != nil {
		return nil, err
	}
	return &RelayActuator{Client: client, Relay: relay}, nil
}

func (a *RelayActuator) SetActive(ctx context.Context, active bool) error {
	if a.Client == nil {
		return fmt.Errorf("relay client is not initialized")
	}
	state := "off"
	if active {
		state = "on"
	}
	env, err := a.Client.SetRelay(ctx, a.Relay, state, "sensorbridge")
	if err != nil {
		return err
	}
	if !env.Success {
		return fmt.Errorf("relay rejected state %s: %s", state, env.Error)
	}
	return nil
}
