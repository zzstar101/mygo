//go:build linux && (amd64 || arm64)

package linux

import (
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"

	"github.com/egoist/mygo/internal/platform"
)

// secrets keeps secrets in the Secret Service (GNOME Keyring, KWallet,
// KeePassXC…) through libsecret, with keytar's schema: the generic one,
// whose attributes are the service and account. libsecret loads with the
// first secret, independently of GTK, so secrets work before Init and
// from any thread: its synchronous calls run their own main context.
type secrets struct{}

// libsecret's functions, loaded once.
type secretAPI struct {
	err error

	hashTableNewFull  func(hash, equal, keyDestroy, valueDestroy ptr) ptr
	hashTableInsert   func(table, key, value ptr) bool
	hashTableUnref    func(table ptr)
	strdup            func(s *byte) ptr
	free              func(p ptr)
	errorFree         func(e ptr)
	strHash, strEqual ptr
	freeFn            ptr

	schemaNewv   func(name *byte, flags int32, attributes ptr) ptr
	schemaUnref  func(schema ptr)
	valueNew     func(secret unsafe.Pointer, length int, contentType *byte) ptr
	valueGet     func(value ptr, length *uint) ptr
	valueUnref   func(value ptr)
	storeBinary  func(schema, attributes ptr, collection, label *byte, value, cancellable ptr, gerr *ptr) bool
	lookupBinary func(schema, attributes, cancellable ptr, gerr *ptr) ptr
	clear        func(schema, attributes, cancellable ptr, gerr *ptr) bool

	// schema is keytar's: org.freedesktop.Secret.Generic with the string
	// attributes service and account.
	schema ptr
}

var libsecret = sync.OnceValue(func() *secretAPI {
	s := &secretAPI{}
	glib, err := open("libglib-2.0.so.0")
	if err != nil {
		s.err = err
		return s
	}
	lib, err := open("libsecret-1.so.0")
	if err != nil {
		s.err = fmt.Errorf("%w (install libsecret: libsecret-1-0 on Debian/Ubuntu, libsecret on Fedora)", err)
		return s
	}
	mustBind(glib, &s.hashTableNewFull, "g_hash_table_new_full")
	mustBind(glib, &s.hashTableInsert, "g_hash_table_insert")
	mustBind(glib, &s.hashTableUnref, "g_hash_table_unref")
	mustBind(glib, &s.strdup, "g_strdup")
	mustBind(glib, &s.free, "g_free")
	mustBind(glib, &s.errorFree, "g_error_free")
	for _, sym := range []struct {
		p    *ptr
		name string
	}{{&s.strHash, "g_str_hash"}, {&s.strEqual, "g_str_equal"}, {&s.freeFn, "g_free"}} {
		if *sym.p, err = purego.Dlsym(glib, sym.name); err != nil {
			s.err = err
			return s
		}
	}
	ok := bind(lib, &s.schemaNewv, "secret_schema_newv") &&
		bind(lib, &s.schemaUnref, "secret_schema_unref") &&
		bind(lib, &s.valueNew, "secret_value_new") &&
		bind(lib, &s.valueGet, "secret_value_get") &&
		bind(lib, &s.valueUnref, "secret_value_unref") &&
		bind(lib, &s.storeBinary, "secret_password_storev_binary_sync") &&
		bind(lib, &s.lookupBinary, "secret_password_lookupv_binary_sync") &&
		bind(lib, &s.clear, "secret_password_clearv_sync")
	if !ok {
		s.err = fmt.Errorf("libsecret is too old: secrets need libsecret 0.19 or later")
		return s
	}
	types := s.hashTableNewFull(s.strHash, s.strEqual, s.freeFn, 0)
	s.hashTableInsert(types, s.strdup(cs("service")), 0) // SECRET_SCHEMA_ATTRIBUTE_STRING
	s.hashTableInsert(types, s.strdup(cs("account")), 0)
	s.schema = s.schemaNewv(cs("org.freedesktop.Secret.Generic"), 0, types) // SECRET_SCHEMA_NONE
	s.hashTableUnref(types)
	return s
})

// attributes returns a new hash table with the attributes of the secret of
// service and account, which the caller unrefs.
func (s *secretAPI) attributes(service, account string) ptr {
	t := s.hashTableNewFull(s.strHash, s.strEqual, s.freeFn, s.freeFn)
	s.hashTableInsert(t, s.strdup(cs("service")), s.strdup(cs(service)))
	s.hashTableInsert(t, s.strdup(cs("account")), s.strdup(cs(account)))
	return t
}

// gErr converts and frees a GError without needing GTK's bindings.
func (s *secretAPI) gErr(e ptr) error {
	if e == 0 {
		return nil
	}
	msg := goStr(*(*ptr)(unsafe.Add(*(*unsafe.Pointer)(unsafe.Pointer(&e)), 8)))
	s.errorFree(e)
	return fmt.Errorf("secret service: %s", msg)
}

func (secrets) SetSecret(service, account string, secret []byte) error {
	s := libsecret()
	if s.err != nil {
		return s.err
	}
	attrs := s.attributes(service, account)
	defer s.hashTableUnref(attrs)
	data := secret
	if len(data) == 0 {
		data = []byte{0} // a valid pointer for an empty secret
	}
	value := s.valueNew(unsafe.Pointer(&data[0]), len(secret), cs("application/octet-stream"))
	runtime.KeepAlive(data)
	defer s.valueUnref(value)
	var gerr ptr
	if !s.storeBinary(s.schema, attrs, nil, cs(service+"/"+account), value, 0, &gerr) {
		if err := s.gErr(gerr); err != nil {
			return err
		}
		return fmt.Errorf("secret service: cannot store the secret")
	}
	return nil
}

func (secrets) Secret(service, account string) ([]byte, error) {
	s := libsecret()
	if s.err != nil {
		return nil, s.err
	}
	attrs := s.attributes(service, account)
	defer s.hashTableUnref(attrs)
	var gerr ptr
	value := s.lookupBinary(s.schema, attrs, 0, &gerr)
	if err := s.gErr(gerr); err != nil {
		if value != 0 {
			s.valueUnref(value)
		}
		return nil, err
	}
	if value == 0 {
		return nil, platform.ErrSecretNotFound
	}
	defer s.valueUnref(value)
	var n uint
	p := s.valueGet(value, &n)
	if p == 0 || n == 0 {
		return []byte{}, nil
	}
	base := *(*unsafe.Pointer)(unsafe.Pointer(&p))
	return append([]byte(nil), unsafe.Slice((*byte)(base), n)...), nil
}

func (secrets) DeleteSecret(service, account string) error {
	s := libsecret()
	if s.err != nil {
		return s.err
	}
	attrs := s.attributes(service, account)
	defer s.hashTableUnref(attrs)
	var gerr ptr
	s.clear(s.schema, attrs, 0, &gerr) // false without an error: nothing to delete
	return s.gErr(gerr)
}
