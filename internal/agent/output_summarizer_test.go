package agent

import (
	"strings"
	"testing"
)

func TestSummarizeOutput_Short(t *testing.T) {
	short := "just a short output\nwith a few lines\n"
	res := SummarizeOutput(short)
	if res != short {
		t.Fatalf("expected short output to be unchanged, got: %s", res)
	}
}

func TestSummarizeOutput_LongNoErrors(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 500; i++ {
		sb.WriteString("this is a normal line with no problems whatsoever\n")
	}
	long := sb.String()
	if len(long) <= 4096 {
		t.Fatalf("test data too short")
	}

	res := SummarizeOutput(long)
	if !strings.Contains(res, "lines omitted") {
		t.Fatalf("expected omitted marker in long output")
	}
	if len(res) >= len(long) {
		t.Fatalf("expected summarized output to be shorter")
	}
}

func TestSummarizeOutput_LongWithErrors(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 500; i++ {
		if i == 250 {
			sb.WriteString("compiler error: undefined variable x\n")
		} else if i == 300 {
			sb.WriteString("fatal: could not find path\n")
		} else {
			sb.WriteString("this is a normal line with no problems whatsoever\n")
		}
	}
	long := sb.String()

	res := SummarizeOutput(long)
	if !strings.Contains(res, "lines omitted") {
		t.Fatalf("expected omitted marker")
	}
	if !strings.Contains(res, "compiler error: undefined variable x") {
		t.Fatalf("expected error line to be preserved")
	}
	if !strings.Contains(res, "fatal: could not find path") {
		t.Fatalf("expected fatal line to be preserved")
	}
}
