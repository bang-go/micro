package metadatax_test

import (
	"context"
	"testing"

	"github.com/bang-go/micro/transport/grpcx/metadatax"
	grpcmetadata "google.golang.org/grpc/metadata"
)

func TestMDBinaryKeyPreservesRawValue(t *testing.T) {
	md := metadatax.MD(grpcmetadata.Pairs())
	want := "foo\x00bar"

	md.Set("trace-bin", want)
	if got := md.Get("trace-bin"); got != want {
		t.Fatalf("Get() = %q, want %q", got, want)
	}

	outgoing := md.ToOutgoing(context.Background())
	extracted := metadatax.ExtractOutgoing(outgoing)
	if got := extracted.Get("trace-bin"); got != want {
		t.Fatalf("ExtractOutgoing().Get() = %q, want %q", got, want)
	}
}

func TestGetReturnsEmptyStringForEmptyValueList(t *testing.T) {
	md := metadatax.MD{"x-empty": {}}

	if got := md.Get("x-empty"); got != "" {
		t.Fatalf("Get() = %q, want empty string", got)
	}
}
