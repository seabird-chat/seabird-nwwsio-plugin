package nwwsio

import (
	"reflect"
	"testing"
)

func TestParseUGC(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []string
	}{
		{
			"zone ranges from a live RWR",
			"ASUS42 KCHS 060200\nRWRCHS\nSCZ027-030-031-034>040-042>045-147>150-060400-\n\nREGIONAL WEATHER ROUNDUP\n",
			[]string{"SCZ027", "SCZ030", "SCZ031", "SCZ034", "SCZ035", "SCZ036", "SCZ037", "SCZ038", "SCZ039", "SCZ040", "SCZ042", "SCZ043", "SCZ044", "SCZ045", "SCZ147", "SCZ148", "SCZ149", "SCZ150"},
		},
		{
			"county list wrapped over two lines and two states",
			"WUUS52 KJAX 060215\nSVRJAX\nFLC031-033-035-089-\nGAC025-039-060245-\n/O.NEW.KJAX.SV.W.0123.260906T0215Z-260906T0245Z/\n\nBULLETIN\n",
			[]string{"FLC031", "FLC033", "FLC035", "FLC089", "GAC025", "GAC039"},
		},
		{
			"multi-state zones with a range",
			"PAZ029-WVZ001>003-060400-\n",
			[]string{"PAZ029", "WVZ001", "WVZ002", "WVZ003"},
		},
		{
			"two segments each with a UGC block",
			"FLZ033-060900-\nfirst segment\n$$\n\nGAZ001-060900-\nsecond segment\n$$\n",
			[]string{"FLZ033", "GAZ001"},
		},
		{
			"prose is not mistaken for UGC",
			"THIS IS A TEST MESSAGE FROM ILM\nCALL 555-0100-200000-\n",
			nil,
		},
	}
	for _, c := range cases {
		if got := ParseUGC(c.text); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: ParseUGC = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestUGCToSAME(t *testing.T) {
	cases := []struct {
		ugc  string
		want []string
	}{
		{"FLC031", []string{"012031"}},
		{"TNZ088", []string{"047157"}},           // zone -> Shelby County
		{"NMZ201", []string{"035031", "035045"}}, // zone spanning two counties
		{"PZZ455", []string{"057455"}},           // Pacific marine zone
		{"LSZ240", []string{"091240"}},           // Lake Superior marine zone
		{"ZZC001", nil},
	}
	for _, c := range cases {
		if got := UGCToSAME(c.ugc); !reflect.DeepEqual(got, c.want) {
			t.Errorf("UGCToSAME(%q) = %v, want %v", c.ugc, got, c.want)
		}
	}
	if n := len(UGCToSAME("DEC000")); n != 3 {
		t.Errorf("UGCToSAME(DEC000) returned %d codes, want the 3 Delaware counties", n)
	}
}
