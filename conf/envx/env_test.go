package envx

import (
	"testing"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  Mode
	}{
		{name: "development alias", input: "Development", want: Development},
		{name: "local alias", input: "local", want: Development},
		{name: "testing alias", input: "testing", want: Test},
		{name: "ci alias", input: "ci", want: Test},
		{name: "production alias", input: "production", want: Production},
		{name: "custom mode", input: "staging", want: Mode("staging")},
		{name: "empty", input: "   ", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Normalize(tt.input); got != tt.want {
				t.Fatalf("Normalize(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestLookupPriority(t *testing.T) {
	t.Setenv(AppEnvKey, "prod")
	t.Setenv(GoEnvKey, "test")

	got, ok := Lookup()
	if !ok {
		t.Fatal("Lookup() ok = false, want true")
	}
	if got != Production {
		t.Fatalf("Lookup() = %q, want %q", got, Production)
	}
}

func TestLookupSkipsBlankValues(t *testing.T) {
	t.Setenv(AppEnvKey, "   ")
	t.Setenv(GoEnvKey, "staging")

	got, ok := Lookup()
	if !ok {
		t.Fatal("Lookup() ok = false, want true")
	}
	if got != Mode("staging") {
		t.Fatalf("Lookup() = %q, want %q", got, Mode("staging"))
	}
}

func TestActiveDefaultsToDevelopment(t *testing.T) {
	t.Setenv(AppEnvKey, "")
	t.Setenv(GoEnvKey, "")

	if got := Active(); got != Development {
		t.Fatalf("Active() = %q, want %q", got, Development)
	}
}

func TestPredicates(t *testing.T) {
	t.Setenv(AppEnvKey, "development")
	t.Setenv(GoEnvKey, "")

	if !IsDev() {
		t.Fatal("IsDev() = false, want true")
	}
	if IsTest() {
		t.Fatal("IsTest() = true, want false")
	}
	if IsProd() {
		t.Fatal("IsProd() = true, want false")
	}
}

func TestHostnameOr(t *testing.T) {
	if got := HostnameOr("fallback"); got == "" {
		t.Fatal("HostnameOr() returned empty hostname")
	}
}
