package engine

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
)

// houseEdge is the fraction of expected value the house retains (1%).
const houseEdge = 0.01

// sampleCrash draws a crash multiplier from the geometric distribution,
// giving the house a ~1% edge. The result is returned in basis points (100 = 1.00x).
// A 1% instant-crash bucket floors the result at 100 (1.00x).
func sampleCrash() int {
	// 1% chance of instant crash at 1.00x
	b := make([]byte, 8)
	rand.Read(b)
	v := new(big.Int).SetBytes(b).Uint64()
	const mod = 1 << 32
	roll := float64(v%mod) / float64(mod) // uniform [0,1)

	if roll < houseEdge {
		return 100 // instant crash: 1.00x
	}

	// Geometric: crash = floor((1-edge)/(1-u) * 100) / 100
	// Shifted to integer basis points:
	// crashBP = floor((1-edge)/(1-roll) * 100)
	crashBP := int((1-houseEdge)/(1-roll)*100) + 1
	if crashBP < 100 {
		crashBP = 100
	}
	return crashBP
}

// FormatMult renders a basis-point multiplier as "X.XX".
func FormatMult(bp int) string {
	return fmt.Sprintf("%d.%02d", bp/100, bp%100)
}

// Commit builds the provably-fair preimage and returns (preimage, sha256hex).
func Commit(gameID int64, crashMultiplierBP int, salt string) (preimage, hash string) {
	preimage = fmt.Sprintf("GAME-%d-MULT-%s-%s", gameID, FormatMult(crashMultiplierBP), salt)
	sum := sha256.Sum256([]byte(preimage))
	hash = hex.EncodeToString(sum[:])
	return
}

// Verify returns true if the given hash matches the commit for the provided inputs.
func Verify(gameID int64, crashMultiplierBP int, salt, commitHash string) bool {
	_, h := Commit(gameID, crashMultiplierBP, salt)
	return h == commitHash
}

// randomSalt generates a 32-byte random hex salt.
func randomSalt() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}
