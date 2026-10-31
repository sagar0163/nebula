package models

import (
	"encoding/json"
	"fmt"
)

// GoalStep represents a single planned action to achieve a goal.
type GoalStep struct {
	Tool  string                 `json:"tool"`
	Input map[string]interface{} `json:"input"`
}

// InputString returns the value for key as a string, handling both string
// and non-string JSON values (e.g. when the model wraps a value in an object).
func (s GoalStep) InputString(key string) string {
	v, ok := s.Input[key]
	if !ok {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	default:
		b, err := json.Marshal(val)
		if err != nil {
			return fmt.Sprintf("%v", val)
		}
		return string(b)
	}
}

// GoalPlan is the sequence of steps to execute.
type GoalPlan struct {
	Steps []GoalStep `json:"steps"`
}
