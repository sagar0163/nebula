package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"

	"github.com/sagar0163/nebula/internal/agent"
	"github.com/sagar0163/nebula/internal/memory"
	"github.com/sagar0163/nebula/internal/models"
	"github.com/sagar0163/nebula/internal/skills"
)

// Step is one unit of work in a workflow.
type Step struct {
	Name      string   `yaml:"name"`
	Skill     string   `yaml:"skill"`
	Prompt    string   `yaml:"prompt"`
	DependsOn []string `yaml:"depends_on"`
}

// Workflow is a named sequence of steps loaded from a YAML file.
type Workflow struct {
	Name  string `yaml:"name"`
	Steps []Step `yaml:"steps"`
}

// LoadFile parses a workflow YAML file.
func LoadFile(path string) (*Workflow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("load workflow: %w", err)
	}
	var wf Workflow
	if err := yaml.Unmarshal(data, &wf); err != nil {
		return nil, fmt.Errorf("parse workflow: %w", err)
	}
	return &wf, nil
}

// Run executes the workflow sequentially, feeding each step's output into
// subsequent steps via template substitution. Returns a map of step outputs.
func (wf *Workflow) Run(ctx context.Context, a *agent.Agent, inputs map[string]string) (map[string]string, error) {
	outputs := make(map[string]string)

	for _, step := range wf.Steps {
		prompt, err := renderPrompt(step.Prompt, inputs, outputs)
		if err != nil {
			return outputs, fmt.Errorf("step %q: render prompt: %w", step.Name, err)
		}

		// Prepend skill instructions when a skill is set.
		if step.Skill != "" {
			sk, err := skills.Load(step.Skill)
			if err == nil && sk.Instructions != "" {
				prompt = sk.Instructions + "\n\n" + prompt
			}
		}

		out, err := a.Ask(ctx, prompt, false)
		if err != nil {
			return outputs, fmt.Errorf("step %q: %w", step.Name, err)
		}
		outputs[step.Name] = out
	}

	return outputs, nil
}

// renderPrompt executes the prompt as a Go template with inputs and prior outputs.
func renderPrompt(promptTmpl string, inputs, outputs map[string]string) (string, error) {
	// Unescape literal \n sequences in YAML scalars.
	promptTmpl = strings.ReplaceAll(promptTmpl, `\n`, "\n")

	tmpl, err := template.New("prompt").Option("missingkey=error").Parse(promptTmpl)
	if err != nil {
		return "", err
	}
	data := map[string]any{
		"input":  inputs,
		"output": outputs,
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// RunBackground executes the workflow in the background.
func (wf *Workflow) RunBackground(ctx context.Context, a *agent.Agent, store memory.Store, inputs map[string]string) (string, error) {
	inputsBytes, _ := json.Marshal(inputs)
	if inputs == nil {
		inputsBytes = []byte("{}")
	}

	job := &models.WorkflowJob{
		WorkflowFile: wf.Name,
		Inputs:       string(inputsBytes),
		Status:       "running",
	}

	if err := store.SaveWorkflowJob(ctx, job); err != nil {
		return "", fmt.Errorf("save job: %w", err)
	}

	go func() {
		bgCtx := context.Background()
		outputs := make(map[string]string)

		for _, step := range wf.Steps {
			job.CurrentStep = step.Name
			_ = store.UpdateWorkflowJob(bgCtx, job)

			prompt, err := renderPrompt(step.Prompt, inputs, outputs)
			if err != nil {
				job.Status = "failed"
				job.Error = fmt.Sprintf("step %q: render prompt: %v", step.Name, err)
				_ = store.UpdateWorkflowJob(bgCtx, job)
				return
			}

			if step.Skill != "" {
				sk, err := skills.Load(step.Skill)
				if err == nil && sk.Instructions != "" {
					prompt = sk.Instructions + "\n\n" + prompt
				}
			}

			out, err := a.Ask(bgCtx, prompt, false)
			if err != nil {
				job.Status = "failed"
				job.Error = fmt.Sprintf("step %q: %v", step.Name, err)
				_ = store.UpdateWorkflowJob(bgCtx, job)
				return
			}
			outputs[step.Name] = out

			outBytes, _ := json.Marshal(outputs)
			job.Output = string(outBytes)
			_ = store.UpdateWorkflowJob(bgCtx, job)
		}

		job.Status = "done"
		job.CurrentStep = ""
		_ = store.UpdateWorkflowJob(bgCtx, job)
	}()

	return job.ID, nil
}
