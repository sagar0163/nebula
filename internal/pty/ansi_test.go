package pty

import "testing"

func TestStripANSI(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain text",
			input: "hello world",
			want:  "hello world",
		},
		{
			name:  "standard colors and reset",
			input: "\x1b[31mError:\x1b[0m \x1b[32mBuild succeeded\x1b[0m",
			want:  "Error: Build succeeded",
		},
		{
			name:  "bold, underline, and compound formatting",
			input: "\x1b[1;4;33mWarning:\x1b[0m deprecated feature",
			want:  "Warning: deprecated feature",
		},
		{
			name:  "256 colors and 24-bit truecolor",
			input: "\x1b[38;5;208mOrange\x1b[0m and \x1b[38;2;255;100;50mTrueColor\x1b[0m",
			want:  "Orange and TrueColor",
		},
		{
			name:  "cursor movements and clear line",
			input: "Compiling...\x1b[2K\x1b[1A\rDone\x1b[10C!",
			want:  "Compiling...\rDone!",
		},
		{
			name:  "OSC window title sequence",
			input: "\x1b]0;Terminal Title\x07Running command...",
			want:  "Running command...",
		},
		{
			name:  "OSC hyperlink sequence",
			input: "See \x1b]8;;https://example.com\x1b\\docs\x1b]8;;\x1b\\ for info",
			want:  "See docs for info",
		},
		{
			name:  "cursor visibility sequences",
			input: "\x1b[?25lLoading...\x1b[?25h",
			want:  "Loading...",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := StripANSI(tc.input)
			if got != tc.want {
				t.Errorf("StripANSI(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
