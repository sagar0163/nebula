package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sagar0163/nebula/internal/harness/shared"
)

// saveTrajectory writes the current trajectory state to disk
func saveTrajectory(workDir string, traj *shared.Trajectory) error {
	nebulaDir := filepath.Join(workDir, ".nebula")
	if err := os.MkdirAll(nebulaDir, 0755); err != nil {
		return err
	}

	path := filepath.Join(nebulaDir, "trajectory.json")
	data, err := json.MarshalIndent(traj, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// loadTrajectory attempts to load a previous trajectory from disk
func loadTrajectory(workDir string) (*shared.Trajectory, error) {
	path := filepath.Join(workDir, ".nebula", "trajectory.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No existing trajectory, which is fine
		}
		return nil, err
	}

	var traj shared.Trajectory
	if err := json.Unmarshal(data, &traj); err != nil {
		return nil, fmt.Errorf("corrupt trajectory file: %w", err)
	}

	return &traj, nil
}
