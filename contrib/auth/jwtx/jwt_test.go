package jwtx

import (
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	// 1. Missing Config
	_, err := New(nil)
	if err == nil {
		t.Error("Expected error when config is nil")
	}

	// 2. Missing SecretKey
	_, err = New(&Config{SecretKey: ""})
	if err == nil {
		t.Error("Expected error when secret key is empty")
	}

	// 3. Valid Config
	j, err := New(&Config{SecretKey: "secret"})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if j == nil {
		t.Error("Expected JWT instance")
	}
}

func TestMustNew(t *testing.T) {
	// 1. Panic on Error
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic when config is invalid")
		}
	}()
	MustNew(&Config{SecretKey: ""})
}

func TestMustNew_Success(t *testing.T) {
	// 2. Success
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Unexpected panic: %v", r)
		}
	}()
	j := MustNew(&Config{SecretKey: "secret", Expire: time.Hour})
	if j == nil {
		t.Error("Expected JWT instance")
	}
}
