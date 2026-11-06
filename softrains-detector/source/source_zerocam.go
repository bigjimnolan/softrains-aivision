package source

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"time"

	"gocv.io/x/gocv"
)

// zerocamSource connects to a ZeroCam server (ZeroCam.c) over TCP.
// It sends an optional ql= quality command followed by a "stream" command,
// then reads the resulting multipart MJPEG stream using JPEG marker scanning.
//
// ZeroCam ql levels (OV2640):
//
//	0 = 160x120,  1 = 176x144,  2 = 320x240,  3 = 352x288
//	4 = 640x480,  5 = 800x600,  6 = 1024x768
type zerocamSource struct {
	addr    string
	quality int // 0 = use camera default (no ql= command sent)
	conn    net.Conn
	reader  *bufio.Reader
}

func newZerocamSource(addr string, quality int) (*zerocamSource, error) {
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("zerocam: failed to connect to %s: %w", addr, err)
	}
	s := &zerocamSource{
		addr:    addr,
		quality: quality,
		conn:    conn,
		reader:  bufio.NewReaderSize(conn, 64*1024),
	}
	if err := s.startStream(); err != nil {
		conn.Close()
		return nil, err
	}
	return s, nil
}

// startStream optionally sets quality, then sends "stream" and drains the
// first HTTP response header block emitted by ZeroCam's net_check path.
func (s *zerocamSource) startStream() error {
	s.conn.SetDeadline(time.Now().Add(10 * time.Second))
	defer s.conn.SetDeadline(time.Time{})

	// Send ql=N first if a quality level was requested.
	// ZeroCam responds: "HTTP/1.1 200 OK\r\n\r\n" then "OK:QLN\r\n"
	if s.quality > 0 {
		cmd := fmt.Sprintf("ql=%d\r\n", s.quality)
		if _, err := s.conn.Write([]byte(cmd)); err != nil {
			return fmt.Errorf("zerocam: failed to send quality command: %w", err)
		}
		// Drain net_check headers (up to blank line) then one OK:QLN ack line.
		if err := s.drainLines(1); err != nil {
			return fmt.Errorf("zerocam: failed to read quality ack: %w", err)
		}
	}

	// Send stream command.
	// ZeroCam responds: "HTTP/1.1 200 OK\r\n\r\n" then the multipart stream headers.
	if _, err := s.conn.Write([]byte("stream\r\n")); err != nil {
		return fmt.Errorf("zerocam: failed to send stream command: %w", err)
	}
	// Drain up to the first blank line (end of net_check response block).
	// The remainder (multipart Content-Type header + frame data) is left in
	// the bufio reader; readJPEGByMarkers will skip past the ASCII headers
	// while scanning for FF D8.
	if err := s.drainLines(0); err != nil {
		return fmt.Errorf("zerocam: failed to read stream response headers: %w", err)
	}
	return nil
}

// drainLines reads lines from the reader until a blank line is seen.
// If extraLines > 0, it then reads that many additional lines after the blank.
func (s *zerocamSource) drainLines(extraLines int) error {
	for {
		line, err := s.reader.ReadString('\n')
		if err != nil {
			return err
		}
		if strings.TrimRight(line, "\r\n") == "" {
			break
		}
	}
	for i := 0; i < extraLines; i++ {
		if _, err := s.reader.ReadString('\n'); err != nil {
			return err
		}
	}
	return nil
}

// Read decodes the next MJPEG frame into frame.
// On consecutive IO errors it attempts a reconnect; on decode failures it
// simply skips to the next frame. Only gives up if reconnection fails.
func (s *zerocamSource) Read(frame *gocv.Mat) bool {
	const maxConsecutiveIOErrors = 3
	ioErrors := 0

	for {
		jpegData, err := s.readNextFrame()
		if err != nil {
			ioErrors++
			if ioErrors >= maxConsecutiveIOErrors {
				if reconnErr := s.reconnect(); reconnErr != nil {
					return false
				}
				ioErrors = 0
			}
			continue
		}
		ioErrors = 0

		buf, err := gocv.IMDecode(jpegData, gocv.IMReadColor)
		if err != nil || buf.Empty() {
			buf.Close()
			continue
		}
		buf.CopyTo(frame)
		buf.Close()
		return true
	}
}

func (s *zerocamSource) readNextFrame() ([]byte, error) {
	s.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	defer s.conn.SetReadDeadline(time.Time{})
	const maxFrameBytes = 2 * 1024 * 1024
	return s.readJPEGByMarkers(maxFrameBytes)
}

func (s *zerocamSource) reconnect() error {
	if s.conn != nil {
		s.conn.Close()
		s.conn = nil
	}
	conn, err := net.DialTimeout("tcp", s.addr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("zerocam: reconnect failed to %s: %w", s.addr, err)
	}
	s.conn = conn
	s.reader = bufio.NewReaderSize(conn, 64*1024)
	if err := s.startStream(); err != nil {
		conn.Close()
		s.conn = nil
		return err
	}
	return nil
}

// readJPEGByMarkers scans the byte stream for an FF D8 ... FF D9 JPEG frame.
// Because ZeroCam reports stale Content-Length values, we ignore headers
// entirely and rely on JPEG SOI/EOI markers.
func (s *zerocamSource) readJPEGByMarkers(maxFrameBytes int) ([]byte, error) {
	data := make([]byte, 0, 64*1024)
	started := false
	var prev byte

	for len(data) < maxFrameBytes {
		b, err := s.reader.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("zerocam: marker read error: %w", err)
		}

		if !started {
			if prev == 0xFF && b == 0xD8 {
				started = true
				data = append(data, 0xFF, 0xD8)
			}
		} else {
			data = append(data, b)
			if data[len(data)-2] == 0xFF && data[len(data)-1] == 0xD9 {
				return data, nil
			}
		}
		prev = b
	}

	return nil, fmt.Errorf("zerocam: did not find complete JPEG frame within %d bytes", maxFrameBytes)
}

func (s *zerocamSource) Close() error {
	if s.conn != nil {
		s.conn.Close()
		s.conn = nil
	}
	return nil
}

func (s *zerocamSource) Describe() string {
	return "zerocam:" + s.addr
}
