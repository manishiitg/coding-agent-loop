package schedulepolicy

import "testing"

func TestPulsePolicyValidation(t *testing.T) {
	for _, tc := range []struct {
		mode, reason string
		valid        bool
	}{
		{"", "Inherited", false}, {"inherit", "Default", false}, {"basic", " \n\t", false},
		{"off", "Owner intentionally disabled post-run stewardship", true},
		{"basic", "Routine queue processing; retain backup and summary", true},
		// full is retired on normal schedules; the full Pulse has its own schedule.
		{"full", "Weekly review of accumulated route evidence", false},
	} {
		if err := ValidatePulse(tc.mode, tc.reason); (err == nil) != tc.valid {
			t.Errorf("%q: %v", tc.mode, err)
		}
	}
}

func TestPulseStampRequiresEverySchedule(t *testing.T) {
	for _, raw := range []string{
		`{"schedules":[{"id":"old","enabled":false}]}`,
		`{"schedules":[{"id":"calendar","schedule_type":"calendar","pulse_mode":"basic"}]}`,
		`{"schedules":[{"pulse_mode":"basic","pulse_mode_reason":"Routine"},{"pulse_mode":"full"}]}`,
	} {
		if err := ValidatePulseStamp([]byte(raw), ExplicitPulseContractVersion); err == nil {
			t.Errorf("incomplete stamp accepted: %s", raw)
		}
		if err := ValidatePulseStamp([]byte(raw), "1.0.40"); err != nil {
			t.Errorf("older migration blocked: %v", err)
		}
	}
	for _, raw := range []string{`{"schedules":[]}`, `{"schedules":[{"pulse_mode":"basic","pulse_mode_reason":"Routine queue processing"}]}`,
		// Persisted legacy full still stamps; it reads as basic.
		`{"schedules":[{"pulse_mode":"full","pulse_mode_reason":"Weekly review"}]}`} {
		if err := ValidatePulseStamp([]byte(raw), ExplicitPulseContractVersion); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPulseContractVersionBoundary(t *testing.T) {
	for _, version := range []string{"", "1.0.40", "invalid"} {
		if RequiresExplicitPulse(version) {
			t.Errorf("unexpected new contract: %s", version)
		}
	}
	for _, version := range []string{"1.0.41", "1.0.42", "1.1.0", "2.0.0"} {
		if !RequiresExplicitPulse(version) {
			t.Errorf("missed contract: %s", version)
		}
	}
}

func TestNormalizePulseReadsLegacyFullAsBasic(t *testing.T) {
	for in, want := range map[string]string{"full": "basic", " FULL ": "basic", "basic": "basic", "off": "off", "": ""} {
		if got := NormalizePulse(in); got != want {
			t.Errorf("NormalizePulse(%q) = %q, want %q", in, got, want)
		}
	}
}
