package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
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
	Name         string             `yaml:"name"`
	Skill        string             `yaml:"skill"`
	Prompt       string             `yaml:"prompt"`
	DependsOn    []string           `yaml:"depends_on"`
	ParsedPrompt *template.Template `yaml:"-"`
}

// Workflow is a named sequence of steps loaded from a YAML file.
type Workflow struct {
	Name  string `yaml:"name"`
	Steps []Step `yaml:"steps"`
}

// LoadFile parses a workflow YAML file and pre-parses all templates.
func LoadFile(path string) (*Workflow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("load workflow: %w", err)
	}
	var wf Workflow
	if err := yaml.Unmarshal(data, &wf); err != nil {
		return nil, fmt.Errorf("parse workflow: %w", err)
	}
	
	// Pre-parse templates
	for i := range wf.Steps {
		promptTmpl := strings.ReplaceAll(wf.Steps[i].Prompt, `\n`, "\n")
		tmpl, err := template.New(wf.Steps[i].Name).Option("missingkey=error").Parse(promptTmpl)
		if err != nil {
			return nil, fmt.Errorf("parse template for step %q: %w", wf.Steps[i].Name, err)
		}
		wf.Steps[i].ParsedPrompt = tmpl
	}
	
	return &wf, nil
}

// Run executes the workflow sequentially, feeding each step's output into
// subsequent steps via template substitution. Returns a map of step outputs.
func (wf *Workflow) Run(ctx context.Context, a *agent.Agent, inputs map[string]string) (map[string]string, error) {
	outputs := make(map[string]string)

	for _, step := range wf.Steps {
		prompt, err := renderPrompt(step.ParsedPrompt, inputs, outputs)
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

// renderPrompt executes the pre-parsed Go template with inputs and prior outputs.
func renderPrompt(tmpl *template.Template, inputs, outputs map[string]string) (string, error) {
	if tmpl == nil {
		return "", fmt.Errorf("template is nil")
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

		updateJob := func(terminal bool) {
			if err := store.UpdateWorkflowJob(bgCtx, job); err != nil {
				log.Printf("warn: update workflow job %s: %v", job.ID, err)
				if terminal {
					if err2 := store.UpdateWorkflowJob(bgCtx, job); err2 != nil {
						log.Printf("warn: retry update workflow job %s: %v", job.ID, err2)
					}
				}
			}
		}

		defer func() {
			if r := recover(); r != nil {
				job.Status = "failed"
				job.Error = fmt.Sprintf("panic: %v", r)
				updateJob(true)
			}
		}()
		outputs := make(map[string]string)

		for _, step := range wf.Steps {
			job.CurrentStep = step.Name
			updateJob(false)

			prompt, err := renderPrompt(step.ParsedPrompt, inputs, outputs)
			if err != nil {
				job.Status = "failed"
				job.Error = fmt.Sprintf("step %q: render prompt: %v", step.Name, err)
				updateJob(true)
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
				updateJob(true)
				return
			}
			outputs[step.Name] = out

			outBytes, _ := json.Marshal(outputs)
			job.Output = string(outBytes)
			updateJob(false)
		}

		job.Status = "done"
		job.CurrentStep = ""
		updateJob(true)
	}()

	return job.ID, nil
}
