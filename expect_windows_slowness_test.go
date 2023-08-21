//go:build windows

package chat_test

import (
	"testing"
	"time"
)

// timeTakenToSleep returns how much time it actually took to call
// time.Sleep() for the specified time.DUration.
// This is useful to detect particular slow program execution.
func timeTakenToSleep(sleepFor time.Duration) time.Duration {
	start := time.Now()
	time.Sleep(sleepFor)
	return time.Since(start)
}

// This test helps detect if/when Windows runners for Github Actions stop being
// 3-5X slower when running `go test`.
func TestWindowsGithubRunnersStillExecuteSlowly(t *testing.T) {
	t.Parallel()
	want := 5 * time.Second
	got := timeTakenToSleep(5 * time.Second)
	if got <= want {
		t.Fatalf("want %v, got %v, expecting this test to take longer to run on Windows Github Runners", want, got)
	}
}
