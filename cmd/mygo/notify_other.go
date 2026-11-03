//go:build !darwin && !linux && !windows

package main

func newNotifier(wake chan<- struct{}) notifier { return noNotifier{} }
