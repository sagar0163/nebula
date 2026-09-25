package providers

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/openai/openai-go"
	"github.com/sagar0163/nebula/internal/llm"
)

func TestApplyResponseFormatJSON(t *testing.T) {
	var params openai.ChatCompletionNewParams
	applyResponseFormat(&params, llm.Request{ResponseFormat: "json"})

	raw, err := params.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	if !strings.Contains(string(raw), `"response_format":{"type":"json_object"}`) {
		t.Fatalf("params missing json_object response_format: %s", raw)
	}
}

func TestApplyResponseFormatLeavesFreeForm(t *testing.T) {
	for _, format := range []string{"", "text", "JSON", "yaml"} {
		var params openai.ChatCompletionNewParams
		applyResponseFormat(&params, llm.Request{ResponseFormat: format})

		raw, err := params.MarshalJSON()
		if err != nil {
			t.Fatalf("marshal params: %v", err)
		}
		if strings.Contains(string(raw), "response_format") {
			t.Errorf("ResponseFormat %q set response_format: %s", format, raw)
		}
	}
}

func TestJSONFormat(t *testing.T) {
	if got := string(jsonFormat(llm.Request{ResponseFormat: "json"})); got != `"json"` {
		t.Errorf("jsonFormat(json) = %s, want %q", got, `"json"`)
	}
	for _, format := range []string{"", "text", "JSON"} {
		if got := jsonFormat(llm.Request{ResponseFormat: format}); got != nil {
			t.Errorf("jsonFormat(%q) = %s, want nil", format, got)
		}
	}
}

// The Ollama hint must marshal as a JSON string, not a bare token.
func TestJSONFormatMarshalsAsJSONString(t *testing.T) {
	got, err := json.Marshal(struct {
		Format json.RawMessage `json:"format,omitempty"`
	}{Format: jsonFormat(llm.Request{ResponseFormat: "json"})})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(got) != `{"format":"json"}` {
		t.Errorf("marshalled = %s, want {\"format\":\"json\"}", got)
	}
}
