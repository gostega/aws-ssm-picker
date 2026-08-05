package main

import "testing"

func TestRegionChoices(t *testing.T) {
	got := regionChoices("ap-southeast-2")
	if len(got) == 0 {
		t.Fatal("no regions offered")
	}
	seen := map[string]bool{}
	for _, r := range got {
		if r == "ap-southeast-2" {
			t.Error("offered the region that just came up empty")
		}
		if seen[r] {
			t.Errorf("duplicate region %q", r)
		}
		seen[r] = true
	}
	if !seen["ap-southeast-1"] {
		t.Error("missing ap-southeast-1 from the fallback list")
	}
}
