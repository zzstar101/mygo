//go:build !js

package mygo

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestSecrets(t *testing.T) {
	const key = "test-token"
	t.Cleanup(func() { Secrets.Delete(key) })

	if _, err := Secrets.Get(key); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("Get of a missing secret: %v, want ErrSecretNotFound", err)
	}
	value := []byte("s3cr\x00t\xff")
	if err := Secrets.Set(key, value); err != nil {
		t.Fatal(err)
	}
	// Stored like keytar: the app's identifier as the service, the key as
	// the account.
	if v, ok := fb.StoredSecret(appID(), key); !ok || !bytes.Equal(v, value) {
		t.Fatalf("stored %q, %v; want %q", v, ok, value)
	}
	got, err := Secrets.Get(key)
	if err != nil || !bytes.Equal(got, value) {
		t.Fatalf("Get = %q, %v; want %q", got, err, value)
	}
	// The caller's slices are its own.
	value[0] = 'X'
	got[1] = 'X'
	if again, _ := Secrets.Get(key); string(again) != "s3cr\x00t\xff" {
		t.Fatalf("the stored secret changed with the caller's slices: %q", again)
	}

	if err := Secrets.Set(key, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if got, _ := Secrets.Get(key); string(got) != "new" {
		t.Fatalf("after replacing, Get = %q", got)
	}
	if err := Secrets.Set(key, nil); err != nil {
		t.Fatal(err)
	}
	if got, err := Secrets.Get(key); err != nil || len(got) != 0 {
		t.Fatalf("empty secret: Get = %q, %v", got, err)
	}

	if err := Secrets.Delete(key); err != nil {
		t.Fatal(err)
	}
	if _, err := Secrets.Get(key); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("Get after Delete: %v, want ErrSecretNotFound", err)
	}
	if err := Secrets.Delete(key); err != nil {
		t.Fatalf("Delete of a missing secret: %v", err)
	}
}

func TestSecretsInvalid(t *testing.T) {
	for _, key := range []string{"", "a\x00b", strings.Repeat("k", maxSecretKeyLen+1)} {
		if err := Secrets.Set(key, []byte("x")); err == nil {
			t.Errorf("Set(%q) succeeded", key)
		}
		if _, err := Secrets.Get(key); err == nil || errors.Is(err, ErrSecretNotFound) {
			t.Errorf("Get(%q) = %v, want an invalid key error", key, err)
		}
		if err := Secrets.Delete(key); err == nil {
			t.Errorf("Delete(%q) succeeded", key)
		}
	}
	long := strings.Repeat("k", maxSecretKeyLen)
	if err := Secrets.Set(long, []byte("x")); err != nil {
		t.Errorf("Set with a key of %d bytes: %v", maxSecretKeyLen, err)
	}
	Secrets.Delete(long)
	if err := Secrets.Set("max", make([]byte, MaxSecretSize)); err != nil {
		t.Errorf("Set of MaxSecretSize bytes: %v", err)
	}
	Secrets.Delete("max")
	if err := Secrets.Set("big", make([]byte, MaxSecretSize+1)); err == nil || !strings.Contains(err.Error(), "MaxSecretSize") {
		t.Errorf("Set of MaxSecretSize+1 bytes: %v", err)
	}
	if _, ok := fb.StoredSecret(appID(), "big"); ok {
		t.Error("a too large secret was stored")
	}
}

func TestSecretsStoreError(t *testing.T) {
	fb.SetSecretError(errors.New("keyring locked"))
	t.Cleanup(func() { fb.SetSecretError(nil) })
	check := func(op string, err error) {
		t.Helper()
		if err == nil || err.Error() != "mygo: secrets: keyring locked" {
			t.Errorf("%s: %v, want mygo: secrets: keyring locked", op, err)
		}
	}
	check("Set", Secrets.Set("k", []byte("v")))
	_, err := Secrets.Get("k")
	check("Get", err)
	check("Delete", Secrets.Delete("k"))
}

// TestSecretsOnMainThread checks that the methods work from the main
// thread of the running app, where they run on another goroutine while
// the loop keeps handling events.
func TestSecretsOnMainThread(t *testing.T) {
	t.Cleanup(func() { Secrets.Delete("main") })
	var got []byte
	var err error
	onMain(func() {
		if err = Secrets.Set("main", []byte("thread")); err == nil {
			got, err = Secrets.Get("main")
		}
	})
	if err != nil || string(got) != "thread" {
		t.Fatalf("Get on the main thread = %q, %v", got, err)
	}
}

func TestSecretsConcurrent(t *testing.T) {
	var wg sync.WaitGroup
	for i := range 8 {
		key := "concurrent-" + string(rune('a'+i))
		t.Cleanup(func() { Secrets.Delete(key) })
		wg.Go(func() {
			for range 20 {
				if err := Secrets.Set(key, []byte(key)); err != nil {
					t.Error(err)
					return
				}
				if v, err := Secrets.Get(key); err != nil || string(v) != key {
					t.Errorf("Get(%q) = %q, %v", key, v, err)
					return
				}
			}
		})
	}
	wg.Wait()
}
