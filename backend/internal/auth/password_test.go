package auth

import "testing"

func TestHashAndVerifyPasswordRoundTrip(t *testing.T) {
	const plain = "correct-horse-battery-staple"

	hash, err := HashPassword(plain)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == "" {
		t.Fatal("expected a non-empty hash")
	}
	if hash == plain {
		t.Fatal("hash must not equal the plaintext password")
	}
	if !VerifyPassword(hash, plain) {
		t.Fatal("expected VerifyPassword to succeed for the correct password")
	}
	if VerifyPassword(hash, "wrong-password") {
		t.Fatal("expected VerifyPassword to fail for an incorrect password")
	}
}

func TestHashPasswordSaltsDifferently(t *testing.T) {
	const plain = "same-password"

	h1, err := HashPassword(plain)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	h2, err := HashPassword(plain)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if h1 == h2 {
		t.Fatal("expected bcrypt salting to produce different hashes for identical input")
	}
	if !VerifyPassword(h1, plain) || !VerifyPassword(h2, plain) {
		t.Fatal("both differently-salted hashes should still verify the same plaintext")
	}
}

func TestVerifyPasswordRejectsMalformedHash(t *testing.T) {
	if VerifyPassword("not-a-real-bcrypt-hash", "anything") {
		t.Fatal("expected VerifyPassword to fail gracefully on a malformed hash")
	}
}
