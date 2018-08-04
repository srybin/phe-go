package phe

import (
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"io"
	"math/big"

	"github.com/Scratch-net/SWU"
	"golang.org/x/crypto/hkdf"
)

var (
	curve = elliptic.P256()
)

type Proof struct {
	Term1, Term2, Term3, Term4, I *Point
	PublicKey                     *Point
	Res                           *big.Int
	Res1, Res2                    *big.Int
}

func RandomZ() (z *big.Int) {
	priv := make([]byte, 32)

	for z == nil {
		io.ReadFull(rand.Reader, priv)

		// If the scalar is out of range, sample another random number.

		if new(big.Int).SetBytes(priv).Cmp(curve.Params().N) >= 0 {
			panic(priv)

		} else {
			z = new(big.Int).SetBytes(priv)
		}
	}
	return
}

func HashZ(data []byte) (z *big.Int) {

	kdf := hkdf.New(sha256.New, data, data, []byte("HashZ"))
	h := make([]byte, 32)
	kdf.Read(h)

	for z == nil {
		// If the scalar is out of range, sample another  number.
		if new(big.Int).SetBytes(h).Cmp(curve.Params().N) >= 0 {
			kdf.Read(h)
		} else {
			z = new(big.Int).SetBytes(h)
		}
	}
	return
}

func GroupHash(data []byte, extraByte byte) *Point {

	x, y := swu.HashToPoint(append(data, extraByte))
	return &Point{x, y}
}
