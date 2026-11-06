package speech

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// Aawaz speaks text through the system audio stack using espeak-ng.
type Aawaz struct {
	command     []string
	timeout     time.Duration
	logger      *logrus.Logger
	mu          sync.Mutex
	beforeSpeak func()
	afterSpeak  func()
}

func NewAawaz(log *logrus.Logger, command []string) *Aawaz {
	return &Aawaz{
		command: command,
		timeout: 100 * time.Second,
		logger:  log,
	}
}

// SetHooks registers callbacks invoked right before and right after speech synthesis
// so the microphone capture stream can release and re-acquire the shared ALSA codec.
func (v *Aawaz) SetHooks(beforeSpeak, afterSpeak func()) {
	if v == nil {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.beforeSpeak = beforeSpeak
	v.afterSpeak = afterSpeak
}

// Speak sends text to the configured speech engine using the bigjim user context.
func (v *Aawaz) Speak(text string) error {
	if v == nil {
		return fmt.Errorf("speech engine is not initialized")
	}

	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return fmt.Errorf("speech text cannot be empty")
	}

	if _, err := exec.LookPath("sudo"); err != nil {
		return fmt.Errorf("sudo wrapper command is not available: %w", err)
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	if v.beforeSpeak != nil {
		v.beforeSpeak()
	}
	if v.afterSpeak != nil {
		defer v.afterSpeak()
	}

	ctx, cancel := context.WithTimeout(context.Background(), v.timeout)
	defer cancel()

	v.logger.Debugf("Executing speech command via sudo for text: %s", trimmed)

	// FIX: Pass the env variables as explicit arguments to sudo so PAM cannot strip them.
	// Syntax matches: sudo -u bigjim XDG_RUNTIME_DIR=... PULSE_SERVER=... espeak-ng "<text>"
	cmd := exec.CommandContext(ctx, v.command[0],
		append(v.command[1:], trimmed)...,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		trimmedOut := strings.TrimSpace(string(output))
		v.logger.Debugf("Error: %v, Output: %s", err, trimmedOut)

		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("speech command timed out after %s", v.timeout)
		}
		if len(trimmedOut) > 0 {
			return fmt.Errorf("speech command failed: %w: %s", err, trimmedOut)
		}
		return fmt.Errorf("speech command failed: %w", err)
	}

	return nil
}
