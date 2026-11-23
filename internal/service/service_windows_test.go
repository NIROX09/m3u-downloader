//go:build windows

package service

import (
	"strings"
	"testing"
)

func TestTaskArguments(t *testing.T) {
	worker := taskArguments(`C:\Program Files\m3u-dl.exe`, "worker")
	if !containsArg(worker, workerTaskName) {
		t.Fatalf("worker task name missing: %v", worker)
	}
	web := taskArguments(`C:\Program Files\m3u-dl.exe`, "web")
	if !containsArg(web, webTaskName) {
		t.Fatalf("web task name missing: %v", web)
	}
	if !containsArg(web, `"C:\Program Files\m3u-dl.exe" web`) {
		t.Fatalf("web task command missing: %v", web)
	}
}

func containsArg(args []string, value string) bool {
	for _, arg := range args {
		if arg == value || strings.Contains(arg, value) {
			return true
		}
	}
	return false
}
