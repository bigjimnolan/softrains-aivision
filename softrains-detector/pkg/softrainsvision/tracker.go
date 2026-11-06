package softrainsvision

//Should be deprecateable
import (
	"image"
	"math"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

const associationPanToleranceDeg = 2.0

// TrackerSnapshot is the parsed detection state emitted by Tracker for downstream actioning.
type TrackerSnapshot struct {
	Detections  []Detection
	Targets     []TrackingTarget
	Timestamp   time.Time
	PanPosition float64 // Current camera pan position for frame validity checking
}

// Tracker is responsible for detection association only.
// Movement/laser decisions are handled by Actioneer.
type Tracker struct {
	Targets      map[int]*TrackingTarget
	targetsMutex sync.RWMutex
	nextID       int
	logger       *logrus.Logger
	publisher    chan<- TrackerSnapshot
}

func NewTracker(frameWidth, frameHeight int, cameraURL string, logger *logrus.Logger) *Tracker {
	return &Tracker{
		Targets: make(map[int]*TrackingTarget),
		nextID:  1,
		logger:  logger,
	}
}

// SetPublisher wires the outbound snapshot channel consumed by Actioneer.
func (t *Tracker) SetPublisher(ch chan<- TrackerSnapshot) {
	t.publisher = ch
}

func (t *Tracker) Update(detections []Detection, panPosition float64) {
	t.targetsMutex.Lock()

	// Remove stale targets first.
	now := time.Now()
	for id, target := range t.Targets {
		if now.Sub(target.LastSeen) > 2*time.Second {
			delete(t.Targets, id)
		}
	}

	// Associate incoming detections with existing targets.
	for _, detection := range detections {
		bestTarget := t.findClosestTargetLocked(detection, panPosition)
		if bestTarget != nil {
			oldCenter := bestTarget.Detection.Center
			bestTarget.Detection = detection
			bestTarget.LastSeen = now
			bestTarget.LastPanPos = panPosition // Track the pan position for this detection
			bestTarget.Velocity = image.Point{
				X: detection.Center.X - oldCenter.X,
				Y: detection.Center.Y - oldCenter.Y,
			}
			continue
		}

		target := &TrackingTarget{
			Detection:  detection,
			LastSeen:   now,
			TrackingID: t.nextID,
			Velocity:   image.Point{X: 0, Y: 0},
			LastPanPos: panPosition, // Record pan position for new detections
		}
		t.Targets[t.nextID] = target
		t.nextID++
	}

	snapshot := TrackerSnapshot{
		Detections:  append([]Detection(nil), detections...),
		Targets:     t.targetsCopyLocked(),
		Timestamp:   now,
		PanPosition: panPosition,
	}
	t.Publish(snapshot)
	t.targetsMutex.Unlock()

}

func (t *Tracker) Publish(snapshot TrackerSnapshot) {
	t.logger.Debug("Publishing tracker snapshot")
	if t.publisher == nil {
		t.logger.Error("Tracker publisher channel is not set; cannot publish snapshot")
		return
	}
	select {
	case t.publisher <- snapshot:
		t.logger.Debug("Tracker snapshot published successfully")
		// Sent successfully
	case <-time.After(1000 * time.Millisecond):
		if t.logger != nil {
			t.logger.Warn("Timeout sending tracker snapshot to Actioneer (slow consumer")
		}
	}
}

func (t *Tracker) GetTargets() []*TrackingTarget {
	t.targetsMutex.RLock()
	defer t.targetsMutex.RUnlock()
	targets := make([]*TrackingTarget, 0, len(t.Targets))
	for _, target := range t.Targets {
		targets = append(targets, target)
	}
	return targets
}

func (t *Tracker) RemoveInactiveTargets(timeout time.Duration) {
	t.targetsMutex.Lock()
	defer t.targetsMutex.Unlock()
	for id, target := range t.Targets {
		if time.Since(target.LastSeen) > timeout {
			delete(t.Targets, id)
		}
	}
}

func (t *Tracker) findClosestTargetLocked(detection Detection, panPosition float64) *TrackingTarget {
	var closestTarget *TrackingTarget
	minDistance := float64(100)
	for _, target := range t.Targets {
		if math.Abs(target.LastPanPos-panPosition) > associationPanToleranceDeg {
			continue
		}
		distance := calculateDistance(detection.Center, target.Detection.Center)
		if distance < minDistance {
			minDistance = distance
			closestTarget = target
		}
	}
	return closestTarget
}

func (t *Tracker) targetsCopyLocked() []TrackingTarget {
	out := make([]TrackingTarget, 0, len(t.Targets))
	for _, target := range t.Targets {
		out = append(out, *target)
	}
	return out
}

func calculateDistance(p1, p2 image.Point) float64 {
	dx := float64(p1.X - p2.X)
	dy := float64(p1.Y - p2.Y)
	return math.Sqrt(dx*dx + dy*dy)
}
