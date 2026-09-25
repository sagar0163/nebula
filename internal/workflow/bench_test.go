package workflow

import (
	"bytes"
	"testing"
	"text/template"
)

func BenchmarkTemplateParsePerRun(b *testing.B) {
	tmplStr := "Analyze this error: {{.input}}\nHere is context: {{.context}}\n"
	data := map[string]string{
		"input":   "cannot find module",
		"context": "package main",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		t, _ := template.New("test").Parse(tmplStr)
		var buf bytes.Buffer
		_ = t.Execute(&buf, data)
	}
}

func BenchmarkTemplatePreParsed(b *testing.B) {
	tmplStr := "Analyze this error: {{.input}}\nHere is context: {{.context}}\n"
	data := map[string]string{
		"input":   "cannot find module",
		"context": "package main",
	}

	t, _ := template.New("test").Parse(tmplStr)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		_ = t.Execute(&buf, data)
	}
}
