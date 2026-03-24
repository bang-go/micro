// Copyright (c) The go-grpc-middleware Authors.
// Licensed under the Apache License 2.0.

package metadatax

import (
	"context"
	"strings"

	grpcMetadata "google.golang.org/grpc/metadata"
)

// MD is a convenience wrapper defining extra functions on the metadata.
type MD grpcMetadata.MD

// ExtractIncoming extracts an inbound metadata from the server-side context.
//
// This function always returns MD wrapper of the grpcMetadata.MD, in case the context doesn't have metadata it returns
// a new empty MD.
func ExtractIncoming(ctx context.Context) MD {
	md, ok := grpcMetadata.FromIncomingContext(ctx)
	if !ok {
		return MD(grpcMetadata.Pairs())
	}
	return MD(md)
}

// ExtractOutgoing extracts an outbound metadata from the client-side context.
//
// This function always returns MD wrapper of the grpcMetadata.MD, in case the context doesn't have metadata it returns
// a new empty MD.
func ExtractOutgoing(ctx context.Context) MD {
	md, ok := grpcMetadata.FromOutgoingContext(ctx)
	if !ok {
		return MD(grpcMetadata.Pairs())
	}
	return MD(md)
}

// Clone performs a *deep* copy of the grpcMetadata.MD.
//
// You can specify the lower-case copiedKeys to only copy certain whitelisted keys. If no keys are explicitly whitelisted
// all keys get copied.
func (m MD) Clone(copiedKeys ...string) MD {
	newMd := MD(grpcMetadata.Pairs())
	for k, vv := range m {
		found := false
		if len(copiedKeys) == 0 {
			found = true
		} else {
			for _, allowedKey := range copiedKeys {
				if strings.EqualFold(allowedKey, k) {
					found = true
					break
				}
			}
		}
		if !found {
			continue
		}
		newMd[k] = make([]string, len(vv))
		copy(newMd[k], vv)
	}
	return newMd
}

// ToOutgoing sets the given MD as a client-side context for dispatching.
func (m MD) ToOutgoing(ctx context.Context) context.Context {
	return grpcMetadata.NewOutgoingContext(ctx, grpcMetadata.MD(m))
}

// ToIncoming sets the given MD as a server-side context for dispatching.
//
// This is mostly useful in ServerInterceptors.
func (m MD) ToIncoming(ctx context.Context) context.Context {
	return grpcMetadata.NewIncomingContext(ctx, grpcMetadata.MD(m))
}

// Get retrieves a single value from the metadata.
//
// It works analogously to httpx.Header.Get, returning the first value if there are many set. If the value is not set,
// an empty string is returned.
//
// The function is binary-key safe.
func (m MD) Get(key string) string {
	k := strings.ToLower(key)
	vv, ok := m[k]
	if !ok || len(vv) == 0 {
		return ""
	}
	return vv[0]
}

// Del deletes all values associated with key from metadata.
//
// It works analogously to httpx.Header.Del, deleting all values if they exist.
func (m MD) Del(key string) MD {
	delete(m, strings.ToLower(key))
	return m
}

// Set sets the given value in a metadata.
//
// It works analogously to httpx.Header.Set, overwriting all previous metadata values.
func (m MD) Set(key string, value string) MD {
	m[strings.ToLower(key)] = []string{value}
	return m
}

// Add appends value to any existing values associated with key.
//
// It works analogously to httpx.Header.Add, as it appends to any existing values associated with key.
func (m MD) Add(key string, value string) MD {
	key = strings.ToLower(key)
	m[key] = append(m[key], value)
	return m
}
