package models

// GoalStep represents a single planned action to achieve a goal.
type GoalStep struct {
	Tool  string            `json:"tool"`
	Input map[string]string `json:"input"`
}

// GoalPlan is the sequence of steps to execute.
type GoalPlan struct {
	Steps []GoalStep `json:"steps"`
}
