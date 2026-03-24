package mqtt

import "testing"

func TestBuildSignaturePassword(t *testing.T) {
	if got, want := BuildSignaturePassword(BuildClientID("group", "device"), "secret"), "DmPrqRl/2JI9UI7EmZbdEDbOor0="; got != want {
		t.Fatalf("BuildSignaturePassword() = %q, want %q", got, want)
	}
}
