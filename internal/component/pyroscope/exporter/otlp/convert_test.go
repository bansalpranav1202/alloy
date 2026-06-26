package otlp

import (
	"testing"

	"github.com/google/pprof/profile"
)

func TestConvertPprofToOTLPRequestUsesPyroscopeProfileNameForMissingPeriodType(t *testing.T) {
	req, err := convertPprofToOTLPRequest(testProfile(&profile.ValueType{}, 0), "wall")
	if err != nil {
		t.Fatalf("convertPprofToOTLPRequest returned error: %v", err)
	}

	p := req.ResourceProfiles[0].ScopeProfiles[0].Profiles[0]
	if got := req.Dictionary.StringTable[p.PeriodType.TypeStrindex]; got != "wall" {
		t.Fatalf("period type = %q, want %q", got, "wall")
	}
	if got := req.Dictionary.StringTable[p.PeriodType.UnitStrindex]; got != "nanoseconds" {
		t.Fatalf("period unit = %q, want %q", got, "nanoseconds")
	}
	if got := p.Period; got != 1 {
		t.Fatalf("period = %d, want %d", got, 1)
	}
}

func TestConvertPprofToOTLPRequestKeepsExistingPeriodType(t *testing.T) {
	req, err := convertPprofToOTLPRequest(testProfile(&profile.ValueType{
		Type: "cpu",
		Unit: "nanoseconds",
	}, 100), "wall")
	if err != nil {
		t.Fatalf("convertPprofToOTLPRequest returned error: %v", err)
	}

	p := req.ResourceProfiles[0].ScopeProfiles[0].Profiles[0]
	if got := req.Dictionary.StringTable[p.PeriodType.TypeStrindex]; got != "cpu" {
		t.Fatalf("period type = %q, want %q", got, "cpu")
	}
	if got := req.Dictionary.StringTable[p.PeriodType.UnitStrindex]; got != "nanoseconds" {
		t.Fatalf("period unit = %q, want %q", got, "nanoseconds")
	}
	if got := p.Period; got != 100 {
		t.Fatalf("period = %d, want %d", got, 100)
	}
}

func testProfile(periodType *profile.ValueType, period int64) *profile.Profile {
	mapping := &profile.Mapping{ID: 1}
	location := &profile.Location{ID: 1, Mapping: mapping}
	return &profile.Profile{
		SampleType: []*profile.ValueType{{
			Type: "wall",
			Unit: "nanoseconds",
		}},
		PeriodType: periodType,
		Period:     period,
		Sample: []*profile.Sample{{
			Location: []*profile.Location{location},
			Value:    []int64{42},
		}},
		Mapping:  []*profile.Mapping{mapping},
		Location: []*profile.Location{location},
	}
}
