package render

import "testing"

// The CPUMeter contract: 0% is empty, even 1% lights one cell, and above 100%
// it caps out full. The width is always meterWidth (the table column never shifts).
func TestCPUMeter(t *testing.T) {
	cases := map[int]string{
		0:   "░░░░░",
		1:   "█░░░░",
		20:  "█░░░░",
		21:  "██░░░",
		50:  "███░░",
		99:  "█████",
		100: "█████",
		250: "█████",
		-1:  "░░░░░",
	}
	for in, want := range cases {
		if got := CPUMeter(in); got != want {
			t.Errorf("CPUMeter(%d) = %q, want %q", in, got, want)
		}
	}
}
