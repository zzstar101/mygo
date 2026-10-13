package mygo

import (
	"errors"
	"fmt"
	"strings"

	"github.com/egoist/mygo/internal/platform"
)

// SecretsModule keeps small secrets, such as access tokens and API keys,
// in the system's credential store. Use the Secrets singleton.
type SecretsModule struct{}

// Secrets keeps small secrets in the system's credential store: the
// login Keychain on macOS, the Secret Service on Linux (GNOME Keyring,
// KWallet, KeePassXC…, through libsecret) and the Credential Manager on
// Windows. The system encrypts them for the user, unlike a file in
// PathUserData:
//
//	if err := mygo.Secrets.Set("github-token", []byte(token)); err != nil { … }
//
//	token, err := mygo.Secrets.Get("github-token")
//	if errors.Is(err, mygo.ErrSecretNotFound) { … ask the user to sign in }
//
// Secrets belong to the app: the service is its identifier (its name
// when it has none, as in development) and the key the account, which is
// how Electron's keytar stores them, so an app moving from Electron finds
// its secrets. Other apps of the user may still read them, on Linux
// without asking.
//
// The methods are safe from any goroutine and work before Run. They may
// block while the system asks the user, e.g. to unlock the keyring or,
// on macOS, to let a rebuilt app read what it stored; on the main thread
// the app keeps handling events meanwhile.
var Secrets SecretsModule

// ErrSecretNotFound is what Secrets.Get fails with when no secret is
// stored for the key.
var ErrSecretNotFound = platform.ErrSecretNotFound

// MaxSecretSize is the largest secret Secrets.Set stores, in bytes: the
// most the Windows Credential Manager keeps, so that an app behaves the
// same everywhere.
const MaxSecretSize = 2560

// maxSecretKeyLen is the longest key, in bytes, well within the
// Credential Manager's 513 characters for the user name.
const maxSecretKeyLen = 256

// Set stores value under key, replacing what it had. The key must not be
// empty or longer than 256 bytes; the value may be at most MaxSecretSize
// bytes.
func (SecretsModule) Set(key string, value []byte) error {
	if err := checkSecretKey(key); err != nil {
		return err
	}
	if len(value) > MaxSecretSize {
		return fmt.Errorf("mygo: secret %q has %d bytes, more than MaxSecretSize (%d)", key, len(value), MaxSecretSize)
	}
	service := appID()
	return offMain(func() error { return wrapSecretErr(backend().Secrets().SetSecret(service, key, value)) })
}

// Get returns the secret stored under key, or ErrSecretNotFound.
func (SecretsModule) Get(key string) ([]byte, error) {
	if err := checkSecretKey(key); err != nil {
		return nil, err
	}
	service := appID()
	type result struct {
		v   []byte
		err error
	}
	r := offMain(func() result {
		v, err := backend().Secrets().Secret(service, key)
		return result{v, wrapSecretErr(err)}
	})
	return r.v, r.err
}

// Delete removes the secret stored under key. Deleting a key without a
// secret is not an error.
func (SecretsModule) Delete(key string) error {
	if err := checkSecretKey(key); err != nil {
		return err
	}
	service := appID()
	return offMain(func() error { return wrapSecretErr(backend().Secrets().DeleteSecret(service, key)) })
}

func checkSecretKey(key string) error {
	switch {
	case key == "":
		return errors.New("mygo: empty secret key")
	case len(key) > maxSecretKeyLen:
		return fmt.Errorf("mygo: secret key has %d bytes, more than %d", len(key), maxSecretKeyLen)
	case strings.ContainsRune(key, 0):
		return fmt.Errorf("mygo: secret key %q contains NUL", key)
	}
	return nil
}

// wrapSecretErr keeps ErrSecretNotFound as is and names the store in
// other errors.
func wrapSecretErr(err error) error {
	if err == nil || errors.Is(err, ErrSecretNotFound) || strings.HasPrefix(err.Error(), "mygo: ") {
		return err
	}
	return fmt.Errorf("mygo: secrets: %w", err)
}

// offMain runs fn, which may block for long, e.g. while the system asks
// the user something, without blocking the event loop: on the main thread
// of a running app fn runs on another goroutine while await handles
// native events. Elsewhere, before Run and after the loop stopped, it
// runs fn directly.
func offMain[T any](fn func() T) T {
	if !isMainThread() || !App.initialized || loop.stopped() {
		return fn()
	}
	ch := make(chan T, 1)
	go func() { deliver(ch, fn()) }()
	return await(ch)
}
