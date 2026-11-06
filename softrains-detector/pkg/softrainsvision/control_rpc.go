package softrainsvision

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/rpc"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"softrains-detector/internal/utils"
	"softrains-detector/pkg/startup"
)

type rpcControlGenericReply struct {
	Ok      bool
	Message string
}

type setAxisRequest struct {
	Axis     string
	Position float64
}

type setSwitchRequest struct {
	ID    int
	State bool
}

type RPCControlClient struct {
	endpoint      string
	tlsCfg        *tls.Config
	timeout       time.Duration
	retryInterval time.Duration
	client        *rpc.Client
	mu            sync.Mutex
	conn          net.Conn
}

type ControllerState struct {
	Pan      float64      `json:"pan"`
	Tilt     float64      `json:"tilt"`
	Roll     float64      `json:"roll"`
	Laser    bool         `json:"laser"`
	Switches map[int]bool `json:"switches"`
}

type StateReply struct {
	ok    bool
	State ControllerState
}

func NewRPCControlClient(control startup.BotControlConfig, timeout time.Duration, retryInterval time.Duration) (*RPCControlClient, error) {
	if control.RPCEndpoint == "" {
		return nil, fmt.Errorf("control rpc endpoint is required when protocol=tls-rpc")
	}
	if control.ServerName == "" {
		return nil, fmt.Errorf("control serverName is required when protocol=tls-rpc")
	}
	if timeout <= 0 {
		timeout = utils.DefaultRetryBudget
	}
	if retryInterval <= 0 {
		retryInterval = utils.DefaultRetryInterval
	}

	rootCAs, err := x509.SystemCertPool()
	if err != nil || rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}
	if control.CACert != "" {
		pemBytes, readErr := os.ReadFile(control.CACert)
		if readErr != nil {
			return nil, fmt.Errorf("read control CA certificate %s: %w", control.CACert, readErr)
		}
		if ok := rootCAs.AppendCertsFromPEM(pemBytes); !ok {
			return nil, fmt.Errorf("parse control CA certificate %s", control.CACert)
		}
	}

	tlsCfg := &tls.Config{
		MinVersion: tls.VersionTLS13,
		MaxVersion: tls.VersionTLS13,
		ServerName: control.ServerName,
		RootCAs:    rootCAs,
	}

	rpcClient := &RPCControlClient{
		endpoint:      control.RPCEndpoint,
		tlsCfg:        tlsCfg,
		timeout:       timeout,
		retryInterval: retryInterval,
	}

	if err := rpcClient.connectLocked(); err != nil {
		return nil, err
	}

	return rpcClient, nil
}

func (c *RPCControlClient) SetAxis(axis string, position float64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	axisReq := &setAxisRequest{
		Axis:     axis,
		Position: position,
	}

	if err := c.callWithRetryLocked(func() error {
		return c.client.Call("GimbalRPC.SetAxis", axisReq, new(struct{}))
	}); err != nil {
		return fmt.Errorf("rpc set axis call failed: %w", err)
	}
	return nil
}

func (c *RPCControlClient) SetSwitch(pin int, state bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	digitalReq := &setSwitchRequest{
		ID:    pin,
		State: state,
	}

	if err := c.callWithRetryLocked(func() error {
		return c.client.Call("GimbalRPC.SetSwitch", digitalReq, new(struct{}))
	}); err != nil {
		return fmt.Errorf("rpc set digital call failed: %w", err)
	}
	return nil
}

func (c *RPCControlClient) GetState() (*StateReply, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var state StateReply
	if err := c.callWithRetryLocked(func() error {
		return c.client.Call("GimbalRPC.GetState", new(struct{}), &state)
	}); err != nil {
		return nil, fmt.Errorf("rpc get state call failed: %w", err)
	}
	return &state, nil
}

func (c *RPCControlClient) SetSpeech(text string) error {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return fmt.Errorf("speech text cannot be empty")
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.callWithRetryLocked(func() error {
		return c.client.Call("GimbalRPC.SetSpeech", trimmed, new(struct{}))
	}); err != nil {
		return fmt.Errorf("rpc speech call failed: %w", err)
	}

	return nil
}

func (c *RPCControlClient) callWithRetryLocked(call func() error) error {
	return utils.Retry(
		context.Background(),
		c.timeout,
		c.retryInterval,
		isRetryableRPCError,
		func() error {
			if err := c.ensureConnLocked(); err != nil {
				return fmt.Errorf("connect failed: %w", err)
			}
			err := call()
			if err != nil {
				c.closeLocked()
			}
			return err
		},
	)
}

func isRetryableRPCError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}
	if netErr, ok := err.(net.Error); ok {
		if netErr.Timeout() {
			return true
		}
		if temporary, ok := interface{}(netErr).(interface{ Temporary() bool }); ok && temporary.Temporary() {
			return true
		}
	}
	errText := strings.ToLower(err.Error())
	return strings.Contains(errText, "unexpected eof") ||
		strings.Contains(errText, "connection is shut down") ||
		strings.Contains(errText, "broken pipe") ||
		strings.Contains(errText, "connection reset by peer")
}
func (c *RPCControlClient) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closeLocked()
}

func (c *RPCControlClient) ensureConnLocked() error {
	if c.conn != nil && c.client != nil {
		return nil
	}
	return c.connectLocked()
}

func (c *RPCControlClient) connectLocked() error {
	c.closeLocked()

	dialer := &net.Dialer{Timeout: c.timeout}
	conn, err := tls.DialWithDialer(dialer, "tcp", c.endpoint, c.tlsCfg)
	if err != nil {
		return fmt.Errorf("dial control rpc endpoint %s: %w", c.endpoint, err)
	}
	c.conn = conn
	c.client = rpc.NewClient(conn)
	return nil
}

func (c *RPCControlClient) closeLocked() {
	if c.client != nil {
		_ = c.client.Close()
		c.client = nil
	}
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
}
