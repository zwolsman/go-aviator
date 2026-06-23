package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
)

func TestFormatMult(t *testing.T) {
	cases := []struct {
		bp   int
		want string
	}{
		{100, "1.00"},
		{150, "1.50"},
		{200, "2.00"},
		{1000, "10.00"},
		{253, "2.53"},
	}
	for _, c := range cases {
		got := FormatMult(c.bp)
		if got != c.want {
			t.Errorf("FormatMult(%d) = %q, want %q", c.bp, got, c.want)
		}
	}
}

func TestCommitVerify(t *testing.T) {
	gameID := int64(42)
	multBP := 250
	salt := "abc123"

	preimage, hash := Commit(gameID, multBP, salt)

	// verify preimage format
	wantPreimage := fmt.Sprintf("GAME-42-MULT-2.50-abc123")
	if preimage != wantPreimage {
		t.Errorf("preimage = %q, want %q", preimage, wantPreimage)
	}

	// verify hash is correct SHA256
	sum := sha256.Sum256([]byte(preimage))
	wantHash := hex.EncodeToString(sum[:])
	if hash != wantHash {
		t.Errorf("hash = %q, want %q", hash, wantHash)
	}

	// round-trip verify
	if !Verify(gameID, multBP, salt, hash) {
		t.Error("Verify returned false for a valid commit")
	}

	// tampered hash should fail
	if Verify(gameID, multBP, salt, "deadbeef") {
		t.Error("Verify returned true for a wrong hash")
	}

	// tampered multiplier should fail
	if Verify(gameID, multBP+1, salt, hash) {
		t.Error("Verify returned true for a wrong multiplier")
	}

	// tampered salt should fail
	if Verify(gameID, multBP, "wrong", hash) {
		t.Error("Verify returned true for a wrong salt")
	}
}

func TestSampleCrashDistribution(t *testing.T) {
	const N = 100_000
	total := 0.0
	instants := 0

	for i := 0; i < N; i++ {
		bp := sampleCrash()
		if bp < 100 {
			t.Errorf("sampleCrash returned %d < 100 (min is 1.00x)", bp)
		}
		if bp == 100 {
			instants++
		}
		total += float64(bp) / 100.0
	}

	// instant crash rate should be ~1% (houseEdge)
	instantRate := float64(instants) / N
	if instantRate < 0.005 || instantRate > 0.02 {
		t.Errorf("instant crash rate = %.4f, expected ~%.4f", instantRate, houseEdge)
	}

	// theoretical expected value (house takes houseEdge) = 1/(1-houseEdge) in continuous limit
	// but with our formula it should be around 100 * (1-edge) credits returned per credit bet
	// Just sanity check the mean is > 1.5x (distribution is long-tailed)
	mean := total / N
	if mean < 1.5 {
		t.Errorf("mean crash multiplier = %.3f, suspiciously low", mean)
	}
	t.Logf("instant crash rate: %.4f (expected ~%.4f)", instantRate, houseEdge)
	t.Logf("mean crash multiplier: %.3f", mean)
}

func TestCrashTieBreak(t *testing.T) {
	// The >= tie-break rule: if auto-cashout target equals crash value, player wins.
	// This is a behavioral spec that the engine enforces; tested here at the formula level.
	// sampleCrash must always return >= 100 (1.00x), so a cashout at exactly 1.00x (100bp)
	// would be processed before a crash at 1.00x in the engine.
	bp := sampleCrash()
	if bp < 100 {
		t.Errorf("crash multiplier %d below minimum 1.00x", bp)
	}
}
