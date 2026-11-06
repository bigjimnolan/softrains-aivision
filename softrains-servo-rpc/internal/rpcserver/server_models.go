package rpcserver

import (
	"net"
	"softrains-servo-rpc/internal/config"
	"softrains-servo-rpc/internal/controller"
	rag "softrains-servo-rpc/internal/rag"
	speechpkg "softrains-servo-rpc/internal/speech"
	"sync"

	"github.com/sirupsen/logrus"
)

type Server struct {
	cfg       *config.Config
	ctrl      *controller.Controller
	listener  net.Listener
	mu        sync.Mutex
	closed    bool
	log       *logrus.Logger
	ragEngine *rag.Engine // Add a reference to the RAG engine
	speech    *speechpkg.Aawaz
}

type SetAxisRequest struct {
	Axis     string
	Position float64
}

type SetSwitchRequest struct {
	ID    int
	State bool
}

type GenericReply struct {
	Ok      bool
	Message string
}

type SaveDataRequest struct {
	ID      string
	Content string
}

type QueryDataRequest struct {
	Query string
}

type QueryDataReply struct {
	Ok               bool
	Message          string
	RetrievedContext []string
	OllamaResponse   string
}

type ControllerState struct {
	Pan      float64      `json:"pan"` // Changed from AxisState to float64 (since controller stores just the position)
	Tilt     float64      `json:"tilt"`
	Roll     float64      `json:"roll"`
	Laser    bool         `json:"laser"`
	Switches map[int]bool `json:"switches"`
}

type StateReply struct {
	Ok    bool
	State ControllerState
}

type PingReply struct {
	Ok      bool
	Message string
}

type SnapshotRequest struct{}

type SnapshotReply struct {
	Ok          bool
	Message     string
	ContentType string
	JPEG        []byte
}

type Service struct {
	ctrl      *controller.Controller
	log       *logrus.Logger
	snapshot  config.SnapshotConfig
	speech    *speechpkg.Aawaz
	cfg       *config.Config
	ragEngine *rag.Engine // Add a reference to the RAG engine
}
