package softrainsvision

const (
	PoseKeyLeftWrist = "left_wrist"
)

// AttachPoseToDetection copies pose keypoints from the RPC payload onto a detection.
func AttachPoseToDetection(detection *Detection, pose map[string]Keypoint) {
	if len(pose) == 0 {
		return
	}
	detection.Pose = make(Pose, len(pose))
	for name, kp := range pose {
		detection.Pose[name] = kp
	}
}

// HasConfidentLeftHand returns true when a left wrist keypoint exists
// and its confidence exceeds the provided threshold.
func HasConfidentLeftHand(pose Pose, minConfidence float32) bool {
	if len(pose) == 0 {
		return false
	}
	leftWrist, ok := pose[PoseKeyLeftWrist]
	if !ok {
		return false
	}
	return leftWrist.Confidence >= minConfidence
}
