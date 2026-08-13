package render

import "testing"

// CPUMeter の約束: 0% は空、1% でも 1 マス立ち、100% 超は満杯で頭打ち。
// 幅は常に meterWidth で揃う (表の列が揺れない)。
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
