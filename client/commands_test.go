package client

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseGeoSubscribeArgs(t *testing.T) {
	cases := []struct {
		name    string
		kind    geoKind
		args    []string
		codes   []string
		filters []string
		errHint string
	}{
		{"same codes with mixed separators and filters", geoKindSAME, []string{"012031,012109", "057455", "Warning,cap"}, []string{"012031", "012109", "057455"}, []string{"Warning", "cap"}, ""},
		{"same code too short", geoKindSAME, []string{"12031", "warning"}, nil, []string{"warning"}, "12031"},
		{"no codes at all", geoKindSAME, []string{"warning"}, nil, []string{"warning"}, "No SAME codes"},
		{"zip+4 normalizes and dedupes", geoKindZIP, []string{"48103-1680", "48103", "all"}, []string{"48103"}, []string{"all"}, ""},
		{"zip without county mapping", geoKindZIP, []string{"00000"}, nil, nil, "00000"},
		{"malformed zip", geoKindZIP, []string{"4810"}, nil, nil, "4810"},
	}
	for _, c := range cases {
		codes, filters, errs := parseGeoSubscribeArgs(c.kind, c.args)
		if !reflect.DeepEqual(codes, c.codes) || !reflect.DeepEqual(filters, c.filters) {
			t.Errorf("%s: codes=%v filters=%v, want %v %v", c.name, codes, filters, c.codes, c.filters)
		}
		if c.errHint == "" && len(errs) != 0 {
			t.Errorf("%s: unexpected errors %v", c.name, errs)
		}
		if c.errHint != "" && (len(errs) != 1 || !strings.Contains(errs[0], c.errHint)) {
			t.Errorf("%s: errs = %v, want one mentioning %q", c.name, errs, c.errHint)
		}
	}
}
