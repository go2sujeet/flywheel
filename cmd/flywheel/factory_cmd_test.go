package main

import (
	"testing"
	"time"
)

func TestClockForEmptyReturnsAdvancingClock(t *testing.T) {
	clock, err := clockFor("")
	if err != nil {
		t.Fatalf("clockFor(\"\"): unexpected error: %v", err)
	}
	first := clock()
	time.Sleep(2 * time.Millisecond)
	second := clock()
	if !second.After(first) {
		t.Fatalf("expected clock to advance, got %v then %v", first, second)
	}
}

func TestClockForRFC3339ReturnsFixedInstant(t *testing.T) {
	want := time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC)
	clock, err := clockFor("2026-09-13T09:00:00Z")
	if err != nil {
		t.Fatalf("clockFor(\"2026-09-13T09:00:00Z\"): unexpected error: %v", err)
	}
	if got := clock(); !got.Equal(want) {
		t.Fatalf("first reading: got %v, want %v", got, want)
	}
	if got := clock(); !got.Equal(want) {
		t.Fatalf("second reading: got %v, want %v", got, want)
	}
}

func TestClockForRejectsBadFlag(t *testing.T) {
	if _, err := clockFor("nope"); err == nil {
		t.Fatal("clockFor(\"nope\"): expected an error, got nil")
	}
}
