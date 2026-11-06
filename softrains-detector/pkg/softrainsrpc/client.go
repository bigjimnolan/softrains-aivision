package softrainsrpc

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"
)

type Client struct {
	endpoint         string
	tlsConfig        *tls.Config
	timeout          time.Duration
	maxResponseBytes int
}

func NewTLSConfig(serverName, caCertPath string) (*tls.Config, error) {
	if serverName == "" {
		return nil, fmt.Errorf("server name is required")
	}

	rootCAs, err := x509.SystemCertPool()
	if err != nil || rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}

	if caCertPath != "" {
		pemBytes, readErr := os.ReadFile(caCertPath)
		if readErr != nil {
			return nil, fmt.Errorf("read CA certificate %s: %w", caCertPath, readErr)
		}
		if ok := rootCAs.AppendCertsFromPEM(pemBytes); !ok {
			return nil, fmt.Errorf("parse CA certificate %s", caCertPath)
		}
	}

	return &tls.Config{
		// Pico/MicroPython mbedTLS builds frequently support TLS 1.2 only.
		// Keep TLS 1.3 available for newer endpoints while allowing 1.2 fallback.
		MinVersion: tls.VersionTLS12,
		MaxVersion: tls.VersionTLS13,
		ServerName: serverName,
		RootCAs:    rootCAs,
	}, nil
}

func NewTLSConfigWithClientCert(serverName, caCertPath, clientCertPath, clientKeyPath string) (*tls.Config, error) {
	tlsCfg, err := NewTLSConfig(serverName, caCertPath)
	if err != nil {
		return nil, err
	}

	if clientCertPath == "" || clientKeyPath == "" {
		return nil, fmt.Errorf("both client certificate and client key are required for mTLS")
	}

	clientCert, err := tls.LoadX509KeyPair(clientCertPath, clientKeyPath)
	if err != nil {
		return nil, fmt.Errorf("load client certificate/key: %w", err)
	}
	tlsCfg.Certificates = []tls.Certificate{clientCert}

	return tlsCfg, nil
}

func NewClient(endpoint string, tlsConfig *tls.Config, timeout time.Duration, maxResponseBytes int) (*Client, error) {
	if endpoint == "" {
		return nil, fmt.Errorf("endpoint is required")
	}
	if tlsConfig == nil {
		return nil, fmt.Errorf("tls config is required")
	}
	if timeout <= 0 {
		// Default to 30s to account for Pico's slow TLS handshake under load
		timeout = 30 * time.Second
	}
	if maxResponseBytes <= 0 {
		maxResponseBytes = 64 * 1024
	}

	return &Client{
		endpoint:         endpoint,
		tlsConfig:        tlsConfig,
		timeout:          timeout,
		maxResponseBytes: maxResponseBytes,
	}, nil
}

func NewClientFromCA(endpoint, serverName, caCertPath string, timeout time.Duration, maxResponseBytes int) (*Client, error) {
	tlsCfg, err := NewTLSConfig(serverName, caCertPath)
	if err != nil {
		return nil, err
	}
	return NewClient(endpoint, tlsCfg, timeout, maxResponseBytes)
}

func NewClientFromCAMTLS(endpoint, serverName, caCertPath, clientCertPath, clientKeyPath string, timeout time.Duration, maxResponseBytes int) (*Client, error) {
	tlsCfg, err := NewTLSConfigWithClientCert(serverName, caCertPath, clientCertPath, clientKeyPath)
	if err != nil {
		return nil, err
	}
	return NewClient(endpoint, tlsCfg, timeout, maxResponseBytes)
}

func (c *Client) Ping(ctx context.Context) error {
	conn, err := c.dial(ctx)
	if err != nil {
		return err
	}
	return conn.Close()
}

func (c *Client) Call(ctx context.Context, header map[string]any, payload []byte) ([]byte, error) {
	if header == nil {
		header = map[string]any{}
	}
	header["payload_size"] = len(payload)

	headerBytes, err := json.Marshal(header)
	if err != nil {
		return nil, fmt.Errorf("marshal header: %w", err)
	}

	conn, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if _, err := conn.Write(append(headerBytes, '\n')); err != nil {
		return nil, fmt.Errorf("write header: %w", err)
	}
	if len(payload) > 0 {
		if _, err := conn.Write(payload); err != nil {
			return nil, fmt.Errorf("write payload: %w", err)
		}
	}

	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("read response line: %w", err)
	}
	if len(line) > c.maxResponseBytes {
		return nil, fmt.Errorf("response exceeded max size (%d bytes)", c.maxResponseBytes)
	}

	if len(line) > 0 && line[len(line)-1] == '\n' {
		line = line[:len(line)-1]
	}
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}

	return line, nil
}

func (c *Client) dial(ctx context.Context) (*tls.Conn, error) {
	// Determine timeout: use context deadline if available, otherwise use client timeout
	timeout := c.timeout
	if dl, ok := ctx.Deadline(); ok {
		// Calculate remaining time from context deadline
		remaining := time.Until(dl)
		if remaining <= 0 {
			return nil, fmt.Errorf("context deadline already exceeded")
		}
		// Use the smaller of context deadline or client timeout
		if remaining < timeout {
			timeout = remaining
		}
	}

	// DialWithDialer respects Timeout for entire operation (TCP + TLS handshake)
	dialer := &net.Dialer{Timeout: timeout}
	conn, err := tls.DialWithDialer(dialer, "tcp", c.endpoint, c.tlsConfig)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", c.endpoint, err)
	}

	// Set connection deadline for subsequent read/write operations
	deadline := time.Now().Add(c.timeout)
	if dl, ok := ctx.Deadline(); ok {
		deadline = dl
	}
	if err := conn.SetDeadline(deadline); err != nil {
		conn.Close()
		return nil, fmt.Errorf("set deadline: %w", err)
	}
	return conn, nil
}
