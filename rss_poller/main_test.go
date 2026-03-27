package main

import "testing"

func TestHostFromFQDN(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"http://notify.demo.svc.cluster.local:3000", "notify.demo.svc.cluster.local:3000"},
		{"https://notify.demo.svc.cluster.local:3000", "notify.demo.svc.cluster.local:3000"},
		{"notify.demo.svc.cluster.local:3000", "notify.demo.svc.cluster.local:3000"},
		{"notify:3000", "notify:3000"},
	}

	for _, tc := range cases {
		got := hostFromFQDN(tc.input)
		if got != tc.want {
			t.Errorf("hostFromFQDN(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
