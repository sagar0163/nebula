package agent

import (
	"strings"
	"testing"

	"github.com/sagar0163/nebula/internal/llm"
)

func TestTrimMessages(t *testing.T) {
	msgs := []llm.Message{{Role: "user", Content: "GOAL"}}
	for i := 0; i < 30; i++ {
		msgs = append(msgs,
			llm.Message{Role: "assistant", Content: `thinking {"steps":[{"tool":"ReadFileTool","input":{"path":"a.py"}}]}`},
			llm.Message{Role: "user", Content: "Tool result for ReadFileTool (step 1/40):\n" + strings.Repeat("x", 3000)})
	}
	got := trimMessages(msgs, 60000, 40000)
	if historySize(got) > 45000 || !strings.HasPrefix(got[0].Content, "GOAL") || len(got) != len(msgs) || got[1].Content != "[Tool result for ReadFileTool(a.py) — elided]" && got[2].Content != "[Tool result for ReadFileTool(a.py) — elided]" {
		t.Fatalf("bad trim: size=%d second=%.80q", historySize(got), got[2].Content)
	}
	for i, m := range got {
		want := "user"
		if i%2 == 1 {
			want = "assistant"
		}
		if i%2 == 1 && m.Content != msgs[i].Content {
			t.Fatalf("assistant message %d was modified", i)
		}
		if m.Role != want {
			t.Fatalf("role alternation broken at %d", i)
		}
	}
}

func TestExtractJSONWithReasoning(t *testing.T) {
	in := "I think {x} is wrong.\n{\"steps\": [{\"tool\": \"DoneTool\", \"input\": {\"reason\": \"ok\"}}]}"
	if got := extractJSON(in); !strings.HasPrefix(got, `{"steps"`) {
		t.Fatalf("got %q", got)
	}
}
