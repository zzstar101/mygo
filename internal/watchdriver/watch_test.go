package watchdriver

import (
	"github.com/egoist/mygo/internal/platform"
	"testing"
	"time"
)

type testDriver struct {
	wake   chan struct{}
	closed chan struct{}
}

func (d *testDriver) Add(platform.FileWatchTarget) error        { return nil }
func (d *testDriver) Remove(uint64)                             {}
func (d *testDriver) Wait() ([]platform.FileWatchNotice, error) { <-d.wake; return nil, nil }
func (d *testDriver) Wake() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}
func (d *testDriver) Close() { close(d.closed) }
func TestRejectedPostReleasesWorker(t *testing.T) {
	d := &testDriver{make(chan struct{}, 1), make(chan struct{})}
	w := New(d, func(func()) bool { return false }, func(platform.FileWatchNotice) { t.Error("unexpected callback") })
	w.Add(platform.FileWatchTarget{ID: 1}, func(error) {})
	select {
	case <-d.closed:
	case <-time.After(time.Second):
		w.Close()
		t.Fatal("rejected post leaked owner worker")
	}
}
