/*
 * Copyright (C) 2015-2018 Virgil Security Inc.
 *
 * All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *     (1) Redistributions of source code must retain the above copyright
 *     notice, this list of conditions and the following disclaimer.
 *
 *     (2) Redistributions in binary form must reproduce the above copyright
 *     notice, this list of conditions and the following disclaimer in
 *     the documentation and/or other materials provided with the
 *     distribution.
 *
 *     (3) Neither the name of the copyright holder nor the names of its
 *     contributors may be used to endorse or promote products derived from
 *     this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE AUTHOR ''AS IS'' AND ANY EXPRESS OR
 * IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED
 * WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
 * DISCLAIMED. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR ANY DIRECT,
 * INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES
 * (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
 * SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
 * HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT,
 * STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING
 * IN ANY WAY OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE
 * POSSIBILITY OF SUCH DAMAGE.
 *
 * Lead Maintainer: Virgil Security Inc. <support@virgilsecurity.com>
 */

package phe

import (
	"crypto/rand"

	"github.com/pkg/errors"
)

// GenerateServerKeypair creates a new random Nist p-256 keypair
func GenerateServerKeypair() ([]byte, error) {
	privateKey := randomZ().Bytes()
	publicKey := new(Point).ScalarBaseMult(privateKey)

	return marshalKeypair(publicKey.Marshal(), privateKey)

}

// GetEnrollment generates a new random enrollment record and a proof
func GetEnrollment(serverKeypair []byte) (*EnrollmentResponse, error) {

	kp, err := unmarshalKeypair(serverKeypair)
	if err != nil {
		return nil, err
	}

	ns := make([]byte, 32)
	_, err = rand.Read(ns)
	if err != nil {
		return nil, err
	}
	hs0, hs1, c0, c1 := eval(kp, ns)
	proof := proveSuccess(kp, hs0, hs1, c0, c1)
	return &EnrollmentResponse{
		NS:    ns,
		C0:    c0.Marshal(),
		C1:    c1.Marshal(),
		Proof: proof,
	}, nil
}

// GetPublicKey returns server public key
func GetPublicKey(serverKeypair []byte) ([]byte, error) {
	key, err := unmarshalKeypair(serverKeypair)
	if err != nil {
		return nil, err
	}

	return key.PublicKey, nil
}

// VerifyPassword compares password attempt to the one server would calculate itself using its private key
// and returns a zero knowledge proof of ether success or failure
func VerifyPassword(serverKeypair []byte, req *VerifyPasswordRequest) (response *VerifyPasswordResponse, err error) {

	kp, err := unmarshalKeypair(serverKeypair)
	if err != nil {
		return nil, err
	}

	if req == nil || len(req.NS) > 32 || len(req.NS) == 0 {
		err = errors.New("Invalid password verify request")
		return
	}

	ns := req.NS

	c0, err := PointUnmarshal(req.C0)
	if err != nil {
		return
	}

	hs0 := hashToPoint(dhs0, ns)
	hs1 := hashToPoint(dhs1, ns)

	if hs0.ScalarMult(kp.PrivateKey).Equal(c0) {
		//password is ok

		c1 := hs1.ScalarMult(kp.PrivateKey)

		response = &VerifyPasswordResponse{
			Res:          true,
			C1:           c1.Marshal(),
			ProofSuccess: proveSuccess(kp, hs0, hs1, c0, c1),
		}
		return
	}

	//password is invalid

	c1, proof, err := proveFailure(kp, c0, hs0)
	if err != nil {
		return
	}

	response = &VerifyPasswordResponse{
		Res:       false,
		C1:        c1.Marshal(),
		ProofFail: proof,
	}

	return
}

func eval(kp *keypair, ns []byte) (hs0, hs1, c0, c1 *Point) {
	hs0 = hashToPoint(dhs0, ns)
	hs1 = hashToPoint(dhs1, ns)

	c0 = hs0.ScalarMult(kp.PrivateKey)
	c1 = hs1.ScalarMult(kp.PrivateKey)
	return
}

func proveSuccess(kp *keypair, hs0, hs1, c0, c1 *Point) *ProofOfSuccess {
	blindX := randomZ()

	term1 := hs0.ScalarMult(blindX.Bytes())
	term2 := hs1.ScalarMult(blindX.Bytes())
	term3 := new(Point).ScalarBaseMult(blindX.Bytes())

	//challenge = group.hash((self.X, self.G, c0, c1, term1, term2, term3), target_type=ZR)

	challenge := hashZ(proofOk, kp.PublicKey, curveG.Marshal(), c0.Marshal(), c1.Marshal(), term1.Marshal(), term2.Marshal(), term3.Marshal())
	res := gf.Add(blindX, gf.MulBytes(kp.PrivateKey, challenge))

	return &ProofOfSuccess{
		Term1:  term1.Marshal(),
		Term2:  term2.Marshal(),
		Term3:  term3.Marshal(),
		BlindX: res.Bytes(),
	}

}

func proveFailure(kp *keypair, c0, hs0 *Point) (c1 *Point, proof *ProofOfFail, err error) {
	r := randomZ()
	minusR := gf.Neg(r)
	minusRX := gf.MulBytes(kp.PrivateKey, minusR)

	c1 = c0.ScalarMult(r.Bytes()).Add(hs0.ScalarMult(minusRX.Bytes()))

	a := r
	b := minusRX

	blindA := randomZ().Bytes()
	blindB := randomZ().Bytes()

	publicKey, err := PointUnmarshal(kp.PublicKey)
	if err != nil {
		return
	}

	// I = (self.X ** a) * (self.G ** b)
	// term1 = c0     ** blind_a
	// term2 = hs0    ** blind_b
	// term3 = self.X ** blind_a
	// term4 = self.G ** blind_b

	term1 := c0.ScalarMult(blindA)
	term2 := hs0.ScalarMult(blindB)
	term3 := publicKey.ScalarMult(blindA)
	term4 := new(Point).ScalarBaseMult(blindB)

	challenge := hashZ(proofError, kp.PublicKey, curveG.Marshal(), c0.Marshal(), c1.Marshal(), term1.Marshal(), term2.Marshal(), term3.Marshal(), term4.Marshal())

	return c1, &ProofOfFail{
		Term1:  term1.Marshal(),
		Term2:  term2.Marshal(),
		Term3:  term3.Marshal(),
		Term4:  term4.Marshal(),
		BlindA: gf.AddBytes(blindA, gf.Mul(challenge, a)).Bytes(),
		BlindB: gf.AddBytes(blindB, gf.Mul(challenge, b)).Bytes(),
	}, nil
}

//Rotate updates server's private and public keys and issues an update token for use on client's side
func Rotate(serverKeypair []byte) (token *UpdateToken, newServerKeypair []byte, err error) {

	kp, err := unmarshalKeypair(serverKeypair)
	if err != nil {
		return
	}
	a, b := randomZ(), randomZ()
	newPrivate := gf.Add(gf.MulBytes(kp.PrivateKey, a), b).Bytes()
	newPublic := new(Point).ScalarBaseMult(newPrivate)

	newServerKeypair, err = marshalKeypair(newPublic.Marshal(), newPrivate)
	if err != nil {
		return
	}

	token = &UpdateToken{
		A: a.Bytes(),
		B: b.Bytes(),
	}

	return
}
