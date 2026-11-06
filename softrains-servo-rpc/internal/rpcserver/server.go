package rpcserver

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"image"
	"image/jpeg"

	"net/rpc"
	"os"

	"strconv"
	"strings"

	"softrains-servo-rpc/internal/config"
	"softrains-servo-rpc/internal/controller"
	rag "softrains-servo-rpc/internal/rag"
	speechpkg "softrains-servo-rpc/internal/speech"

	"github.com/blackjack/webcam"
	"github.com/sirupsen/logrus"
)

// Common V4L2 pixel format FourCC codes
const (
	V4L2_PIX_FMT_MJPEG = 0x47504a4d // 'MJPG'
	V4L2_PIX_FMT_YUYV  = 0x56595559 // 'YUYV'
)

func New(cfg *config.Config, ctrl *controller.Controller, log *logrus.Logger, ragHandle *rag.Engine, speechEngine ...*speechpkg.Aawaz) (*Server, error) {
	tlsCfg, err := tlsConfigFrom(cfg)
	if err != nil {
		return nil, err
	}
	ln, err := tls.Listen("tcp", cfg.ListenAddress, tlsCfg)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", cfg.ListenAddress, err)
	}
	var talker *speechpkg.Aawaz
	if len(speechEngine) > 0 && speechEngine[0] != nil {
		talker = speechEngine[0]
	} else {
		talker = speechpkg.NewAawaz(log, cfg.SpeakCommand)
	}
	return &Server{cfg: cfg, ctrl: ctrl, listener: ln, log: log, ragEngine: ragHandle, speech: talker}, nil
}

func (s *Server) Serve() error {
	rpcSrv := rpc.NewServer()
	talker := s.speech
	if talker == nil {
		talker = speechpkg.NewAawaz(s.log, s.cfg.SpeakCommand)
	}
	svc := &Service{
		ctrl:      s.ctrl,
		log:       s.log,
		snapshot:  s.cfg.Snapshot,
		speech:    talker,
		cfg:       s.cfg,
		ragEngine: s.ragEngine, // Pass the RAG engine to the service
	}
	if err := rpcSrv.RegisterName("GimbalRPC", svc); err != nil {
		return fmt.Errorf("register rpc service: %w", err)
	}
	s.log.Infof("rpc server started on %s", s.cfg.ListenAddress)
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed {
				return nil
			}
			return fmt.Errorf("accept rpc connection: %w", err)
		}
		s.log.Debugf("accepted new rpc connection from %s", conn.RemoteAddr())
		go rpcSrv.ServeConn(conn)
	}
}

func (s *Server) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}

func (svc *Service) SetAxis(req SetAxisRequest, reply *GenericReply) error {
	if err := svc.ctrl.SetAxis(req.Axis, req.Position); err != nil {
		reply.Ok = false
		reply.Message = err.Error()
		return nil
	}
	reply.Ok = true
	reply.Message = "axis updated"
	return nil
}

func (svc *Service) SetSwitch(req SetSwitchRequest, reply *GenericReply) error {
	if err := svc.ctrl.SetSwitch(req.ID, req.State); err != nil {
		reply.Ok = false
		reply.Message = err.Error()
		return nil
	}
	reply.Ok = true
	reply.Message = "switch updated"
	return nil
}

func (svc *Service) GetState(_ struct{}, reply *StateReply) error {
	state := svc.ctrl.State()

	// Safely convert the state to our concrete type
	reply.Ok = true

	// Handle pan axis - controller returns just position as float64
	if panVal, ok := state["pan"]; ok {
		if panFloat, ok := panVal.(float64); ok {
			reply.State.Pan = panFloat
		}
	}

	// Handle tilt axis
	if tiltVal, ok := state["tilt"]; ok {
		if tiltFloat, ok := tiltVal.(float64); ok {
			reply.State.Tilt = tiltFloat
		}
	}

	// Handle roll axis
	if rollVal, ok := state["roll"]; ok {
		if rollFloat, ok := rollVal.(float64); ok {
			reply.State.Roll = rollFloat
		}
	}

	// Handle laser
	if laserVal, ok := state["laser"]; ok {
		if laserBool, ok := laserVal.(bool); ok {
			reply.State.Laser = laserBool
		}
	}

	// Handle switches - convert map[string]interface{} to map[int]bool
	switchesMap := make(map[int]bool)
	if switchesVal, ok := state["switches"]; ok {
		if switchesInterface, ok := switchesVal.(map[string]interface{}); ok {
			for k, v := range switchesInterface {
				if intKey, err := strconv.Atoi(k); err == nil {
					if boolVal, ok := v.(bool); ok {
						switchesMap[intKey] = boolVal
					}
				}
			}
		}
	}
	reply.State.Switches = switchesMap

	return nil
}

func (svc *Service) Ping(_ struct{}, reply *PingReply) error {
	reply.Ok = true
	reply.Message = "pong"
	return nil
}

func (svc *Service) SetSpeech(text string, reply *GenericReply) error {
	if svc.speech == nil {
		svc.speech = speechpkg.NewAawaz(svc.log, svc.cfg.SpeakCommand)
	}

	if err := svc.speech.Speak(text); err != nil {
		reply.Ok = false
		reply.Message = err.Error()
		return nil
	}

	reply.Ok = true
	reply.Message = "speech sent"
	return nil
}

// SaveData indexes document data into Bleve
func (svc *Service) SaveData(req SaveDataRequest, reply *GenericReply) error {
	doc := rag.Document{
		ID:      req.ID,
		Content: req.Content,
	}
	svc.log.Infof("Indexing document ID: %s %s", doc.ID, doc.Content)
	// Adjust ragEngine location depending on where you store it in your Service struct
	if err := svc.ragEngine.IndexDocument(doc); err != nil {
		reply.Ok = false
		reply.Message = fmt.Sprintf("failed to index document: %v", err)
		return nil // Return nil so the RPC framework sends the formatted error inside the reply
	}

	reply.Ok = true
	reply.Message = "data successfully indexed"
	return nil
}

// QueryData searches Bleve and pipes context into Ollama
func (svc *Service) QueryData(req QueryDataRequest, reply *QueryDataReply) error {
	// 1. Retrieve text chunks from Bleve
	contexts, err := svc.ragEngine.Search(req.Query)
	if err != nil {
		reply.Ok = false
		reply.Message = fmt.Sprintf("retrieval failed: %v", err)
		return nil
	}
	reply.RetrievedContext = contexts
	for i, ctx := range contexts {
		svc.log.Debugf("Context chunk %d: %s", i+1, ctx)
	}

	// 2. Format Context & Prompt payload
	contextBlock := strings.Join(contexts, "\n---\n")
	reply.Ok = true
	reply.Message = "retrieval successful"
	reply.RetrievedContext = []string{fmt.Sprintf(
		"Answer the query using only the provided context.\n\nContext:\n%s\n\nQuery: %s",
		contextBlock,
		req.Query,
	)}
	return nil
}

func (svc *Service) GetSnapshotJPEG(_ SnapshotRequest, reply *SnapshotReply) error {
	if !svc.snapshot.Enabled {
		reply.Ok = false
		reply.Message = "snapshot RPC is disabled"
		return nil
	}

	if svc.snapshot.Device == "" {
		reply.Ok = false
		reply.Message = "snapshot device is not configured"
		return nil
	}

	// 1. Initialize native V4L2 interface
	cam, err := webcam.Open(svc.snapshot.Device)
	if err != nil {
		reply.Ok = false
		reply.Message = fmt.Sprintf("failed to open camera device: %v", err)
		return nil
	}
	defer cam.Close()

	// 2. Resolve pixel format (Default to high-perf hardware MJPEG)
	pixelFormat := webcam.PixelFormat(V4L2_PIX_FMT_MJPEG)
	isYUYV := strings.ToLower(svc.snapshot.InputFormat) == "yuyv"
	if isYUYV {
		pixelFormat = webcam.PixelFormat(V4L2_PIX_FMT_YUYV)
	}

	// 3. Fallback resolution configuration rules
	width, height := uint32(1280), uint32(720)
	if svc.snapshot.Width > 0 && svc.snapshot.Height > 0 {
		width = uint32(svc.snapshot.Width)
		height = uint32(svc.snapshot.Height)
	}

	_, _, _, err = cam.SetImageFormat(pixelFormat, width, height)
	if err != nil {
		reply.Ok = false
		reply.Message = fmt.Sprintf("failed to set format or resolution: %v", err)
		return nil
	}

	// 4. Fire up the hardware stream mapping
	err = cam.StartStreaming()
	if err != nil {
		reply.Ok = false
		reply.Message = fmt.Sprintf("failed to start camera streaming: %v", err)
		return nil
	}
	defer cam.StopStreaming()

	// 5. Purge initial unexposed frames (Hardware warmup loop)
	for i := 0; i < 15; i++ {
		if err := cam.WaitForFrame(1); err == nil {
			if frame, err := cam.ReadFrame(); err == nil {
				_ = frame // Discard
			}
		}
	}

	// 6. Handle timeout constraint conversion safely
	timeoutSec := uint32(1)
	if svc.snapshot.TimeoutMs > 1000 {
		timeoutSec = uint32(svc.snapshot.TimeoutMs / 1000)
	}

	if err := cam.WaitForFrame(timeoutSec); err != nil {
		reply.Ok = false
		reply.Message = fmt.Sprintf("timeout waiting for valid video frame: %v", err)
		return nil
	}

	frameData, err := cam.ReadFrame()
	if err != nil {
		reply.Ok = false
		reply.Message = fmt.Sprintf("failed reading target frame: %v", err)
		return nil
	}

	if len(frameData) == 0 {
		reply.Ok = false
		reply.Message = "camera returned empty frame payload"
		return nil
	}

	// 7. Enforce clean JPEG payload generation
	var finalJPEG []byte

	if isYUYV {
		// If input is raw YUYV bytes, decode it to an image and compress to JPEG in software
		img := yuyvToYCbCr(frameData, int(width), int(height))
		var buf bytes.Buffer

		// Set quality mapping (fallback to 85 if not configured)
		quality := svc.snapshot.Quality
		if quality <= 0 {
			quality = 85
		}

		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
			reply.Ok = false
			reply.Message = fmt.Sprintf("failed to software encode JPEG payload: %v", err)
			return nil
		}
		finalJPEG = buf.Bytes()
	} else {
		// Hardware already provided a perfect JPEG byte array; clone buffer away from reuse safety
		finalJPEG = append([]byte(nil), frameData...)
	}

	// 8. Size boundary enforcement check
	if int64(len(finalJPEG)) > svc.snapshot.MaxBytes {
		reply.Ok = false
		reply.Message = fmt.Sprintf("snapshot frame size (%d bytes) exceeded maxBytes limit", len(finalJPEG))
		return nil
	}

	// 9. Standardize output responses
	reply.Ok = true
	reply.ContentType = "image/jpeg"
	reply.JPEG = finalJPEG
	reply.Message = "ok"
	return nil
}

// Helper to convert uncompressed YUYV (YUV422) macro-pixels into standard Go image formats
func yuyvToYCbCr(data []byte, w, h int) image.Image {
	rect := image.Rect(0, 0, w, h)
	img := image.NewYCbCr(rect, image.YCbCrSubsampleRatio422)

	for y := 0; y < h; y++ {
		for x := 0; x < w; x += 2 {
			// YUYV structure maps 4 bytes into 2 adjacent pixels sharing chromaticity values
			baseIdx := (y*w + x) * 2
			if baseIdx+3 >= len(data) {
				break
			}

			y0 := data[baseIdx]
			u := data[baseIdx+1]
			y1 := data[baseIdx+2]
			v := data[baseIdx+3]

			// Mapping back to Go YCbCr component matrices
			offY0 := y*img.YStride + x
			offY1 := y*img.YStride + x + 1
			offC := y*img.CStride + x/2

			if offY1 < len(img.Y) && offC < len(img.Cb) {
				img.Y[offY0] = y0
				img.Y[offY1] = y1
				img.Cb[offC] = u
				img.Cr[offC] = v
			}
		}
	}
	return img
}

func tlsConfigFrom(cfg *config.Config) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(cfg.TLS.CertFile, cfg.TLS.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load server cert/key: %w", err)
	}
	tlsCfg := &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
	}

	if cfg.TLS.ClientCAFile != "" {
		pemBytes, readErr := os.ReadFile(cfg.TLS.ClientCAFile)
		if readErr != nil {
			return nil, fmt.Errorf("read client ca file %s: %w", cfg.TLS.ClientCAFile, readErr)
		}
		clientCAs := x509.NewCertPool()
		if ok := clientCAs.AppendCertsFromPEM(pemBytes); !ok {
			return nil, fmt.Errorf("parse client ca file %s", cfg.TLS.ClientCAFile)
		}
		tlsCfg.ClientCAs = clientCAs
		if cfg.TLS.RequireClientCert {
			tlsCfg.ClientAuth = tls.RequireAndVerifyClientCert
		} else {
			tlsCfg.ClientAuth = tls.VerifyClientCertIfGiven
		}
	}

	return tlsCfg, nil
}
