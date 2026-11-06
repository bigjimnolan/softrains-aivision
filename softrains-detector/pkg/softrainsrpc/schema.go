package softrainsrpc

import (
	"encoding/json"
	"fmt"
)

const (
	SchemaV1 = "softrains-rpc-v1"
)

type Envelope struct {
	Schema    string          `json:"schema"`
	Service   string          `json:"service"`
	Command   string          `json:"command"`
	RequestID string          `json:"request_id,omitempty"`
	Success   bool            `json:"success"`
	Error     string          `json:"error,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
}

func (e *Envelope) DecodeResult(target any) error {
	if len(e.Result) == 0 {
		return fmt.Errorf("rpc response has no result payload")
	}
	if err := json.Unmarshal(e.Result, target); err != nil {
		return fmt.Errorf("decode result: %w", err)
	}
	return nil
}

type Request struct {
	Schema      string         `json:"schema,omitempty"`
	Service     string         `json:"service,omitempty"`
	Command     string         `json:"command"`
	RequestID   string         `json:"request_id,omitempty"`
	PayloadSize int            `json:"payload_size"`
	Params      map[string]any `json:"params,omitempty"`
}

func NewRequest(service, command, requestID string, payloadSize int, params map[string]any) Request {
	if payloadSize < 0 {
		payloadSize = 0
	}
	return Request{
		Schema:      SchemaV1,
		Service:     service,
		Command:     command,
		RequestID:   requestID,
		PayloadSize: payloadSize,
		Params:      params,
	}
}

func NormalizeResponse(raw []byte, defaultService, defaultCommand, defaultRequestID string) (Envelope, error) {
	var env Envelope
	if err := json.Unmarshal(raw, &env); err == nil && env.Command != "" && env.Schema != "" {
		if env.Service == "" {
			env.Service = defaultService
		}
		if env.RequestID == "" {
			env.RequestID = defaultRequestID
		}
		if len(env.Result) == 0 && env.Error == "" {
			env.Result = json.RawMessage(raw)
		}
		return env, nil
	}

	// Legacy payload fallback: convert to strict envelope for callers.
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return Envelope{}, fmt.Errorf("parse response json: %w", err)
	}

	env = Envelope{
		Schema:    SchemaV1,
		Service:   defaultService,
		Command:   defaultCommand,
		RequestID: defaultRequestID,
		Success:   true,
		Result:    json.RawMessage(raw),
	}

	if v, ok := generic["success"].(bool); ok {
		env.Success = v
	}
	if v, ok := generic["error"].(string); ok && v != "" {
		env.Success = false
		env.Error = v
	}

	return env, nil
}
