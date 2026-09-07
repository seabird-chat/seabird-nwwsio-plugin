package nwwsio

import "testing"

func TestProductBody(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{
			"NWWS-OI double spacing collapsed and envelope removed",
			"\n\n974\n\nWWUS82 KILM 061930\n\nSPSILM\n\n\n\nSpecial Weather Statement\n\nNational Weather Service Wilmington NC\n\n330 PM EDT Sun Sep 6 2026\n\n \n\nNCZ087-096-099-062015-\n\nRobeson NC-\n\n",
			"Special Weather Statement\nNational Weather Service Wilmington NC\n330 PM EDT Sun Sep 6 2026\n\nNCZ087-096-099-062015-\nRobeson NC-",
		},
		{
			"single-spaced envelope with a BBB indicator",
			"\n384\nSRUS42 KJAX 060329 CCA\nRRMJAX\n\n.A DATA\n",
			".A DATA",
		},
		{
			"text without an envelope is untouched",
			"Hello\n\nWorld",
			"Hello\n\nWorld",
		},
	}
	for _, c := range cases {
		if got := ProductBody(c.text); got != c.want {
			t.Errorf("%s: ProductBody = %q, want %q", c.name, got, c.want)
		}
	}
}
