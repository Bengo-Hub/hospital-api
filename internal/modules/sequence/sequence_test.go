package sequence

import (
	"testing"
	"time"
)

func TestDefaultFormat(t *testing.T) {
	if got := DefaultFormat(KindMRN); got != "{prefix}/{seq}/{yy}" {
		t.Errorf("DefaultFormat(KindMRN) = %q, want {prefix}/{seq}/{yy}", got)
	}
	if got := DefaultFormat(KindVisitNumber); got != "{prefix}{seq}" {
		t.Errorf("DefaultFormat(KindVisitNumber) = %q, want {prefix}{seq}", got)
	}
	if got := DefaultFormat("unknown_kind"); got != "{prefix}{seq}" {
		t.Errorf("DefaultFormat(unknown) = %q, want the generic fallback", got)
	}
}

func TestDefaultReset(t *testing.T) {
	if got := defaultReset(KindMRN); got != ResetYearly {
		t.Errorf("defaultReset(KindMRN) = %q, want yearly", got)
	}
	if got := defaultReset(KindVisitNumber); got != ResetNone {
		t.Errorf("defaultReset(KindVisitNumber) = %q, want none", got)
	}
}

func TestPeriodKey(t *testing.T) {
	now := time.Date(2026, 8, 29, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		reset string
		want  string
	}{
		{ResetYearly, "2026"},
		{ResetMonthly, "2026-08"},
		{ResetNone, ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := periodKey(c.reset, now); got != c.want {
			t.Errorf("periodKey(%q, ...) = %q, want %q", c.reset, got, c.want)
		}
	}
}

func TestRender(t *testing.T) {
	now := time.Date(2026, 8, 29, 10, 0, 0, 0, time.UTC)

	cases := []struct {
		name     string
		format   string
		prefix   string
		padWidth int
		val      int64
		want     string
	}{
		{"mrn template with year", "{prefix}/{seq}/{yy}", "MRN", 6, 1, "MRN/000001/26"},
		{"visit number template no separators", "{prefix}{seq}", "V", 6, 42, "V000042"},
		{"empty format falls back to prefix+seq", "", "MRN", 5, 7, "MRN00007"},
		{"zero pad width falls back to 5", "{prefix}{seq}", "X", 0, 3, "X00003"},
		{"full year and month placeholders", "{prefix}-{yyyy}-{mm}-{seq}", "P", 4, 9, "P-2026-08-0009"},
		{"large value not truncated by pad width", "{prefix}{seq}", "V", 3, 123456, "V123456"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Render(c.format, c.prefix, c.padWidth, c.val, now); got != c.want {
				t.Errorf("Render(%q, %q, %d, %d) = %q, want %q", c.format, c.prefix, c.padWidth, c.val, got, c.want)
			}
		})
	}
}
