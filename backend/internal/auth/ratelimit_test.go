package auth

import (
	"testing"
	"time"
)

func TestRateLimiterTripsAfterThreshold(t *testing.T) {
	rl := NewRateLimiter(3, time.Minute)

	for i := 0; i < 3; i++ {
		if !rl.Allow("alice") {
			t.Fatalf("attempt %d should be allowed within the threshold", i+1)
		}
	}

	if rl.Allow("alice") {
		t.Fatal("expected the attempt exceeding the threshold to be rejected")
	}
	if rl.Allow("alice") {
		t.Fatal("expected subsequent attempts to remain rejected within the window")
	}
}

func TestRateLimiterKeysAreIndependent(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute)

	if !rl.Allow("alice") {
		t.Fatal("first attempt for alice should be allowed")
	}
	if rl.Allow("alice") {
		t.Fatal("second attempt for alice should be rejected")
	}
	if !rl.Allow("bob") {
		t.Fatal("bob should have his own independent budget")
	}
}

func TestRateLimiterWindowExpiry(t *testing.T) {
	rl := NewRateLimiter(1, 20*time.Millisecond)

	if !rl.Allow("carol") {
		t.Fatal("first attempt should be allowed")
	}
	if rl.Allow("carol") {
		t.Fatal("second attempt within the window should be rejected")
	}

	time.Sleep(40 * time.Millisecond)

	if !rl.Allow("carol") {
		t.Fatal("attempt after the window has elapsed should be allowed again")
	}
}

func TestDefaultLoginRateLimiterThreshold(t *testing.T) {
	rl := DefaultLoginRateLimiter()

	for i := 0; i < 5; i++ {
		if !rl.Allow("someuser") {
			t.Fatalf("attempt %d should be allowed under the default 5/5min policy", i+1)
		}
	}
	if rl.Allow("someuser") {
		t.Fatal("6th attempt within the default window should be rejected")
	}
}
