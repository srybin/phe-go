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
	"crypto/sha512"
	"math/big"

	"github.com/pkg/errors"
	"golang.org/x/crypto/hkdf"
)

// Client is responsible for protecting & checking passwords at the client (website) side
type Client struct {
	clientPrivateKey      *big.Int
	clientPrivateKeyBytes []byte
	serverPublicKey       *Point
	serverPublicKeyBytes  []byte
}

// GenerateClientKey creates a new random key used on the Client side
func GenerateClientKey() []byte {
	return randomZ().Bytes()
}

//NewClient creates new client instance using client's private key and server's public key used for verification
func NewClient(privateKey []byte, serverPublicKey []byte) (*Client, error) {
	if len(privateKey) == 0 {
		return nil, errors.New("invalid private key")
	}

	pub, err := PointUnmarshal(serverPublicKey)

	if err != nil {
		return nil, errors.Wrap(err, "invalid public key")
	}

	return &Client{
		clientPrivateKey:      new(big.Int).SetBytes(privateKey),
		serverPublicKey:       pub,
		clientPrivateKeyBytes: privateKey,
		serverPublicKeyBytes:  serverPublicKey,
	}, nil

}

// EnrollAccount uses fresh Enrollment Response and user's password (or its hash) to create a new Enrollment Record which
// is then supposed to be stored in a database
// it also generates a random encryption key which can be used to protect user's data
func (c *Client) EnrollAccount(password []byte, resp *EnrollmentResponse) (rec *EnrollmentRecord, key []byte, err error) {

	if resp == nil {
		err = errors.New("invalid proof")
		return
	}

	c0, err := PointUnmarshal(resp.C0)
	if err != nil {
		return
	}

	c1, err := PointUnmarshal(resp.C1)
	if err != nil {
		return
	}

	proofValid := c.validateProofOfSuccess(resp.Proof, resp.NS, c0, c1, resp.C0, resp.C1)
	if !proofValid {
		err = errors.New("invalid proof")
		return
	}

	// client nonce and 2 points
	nc := make([]byte, 32)
	_, err = rand.Read(nc)
	if err != nil {
		panic(err)
	}
	hc0 := hashToPoint(dhc0, nc, password)
	hc1 := hashToPoint(dhc1, nc, password)

	// encryption key in a form of a random point
	mBuf := make([]byte, 32)
	_, err = rand.Read(mBuf)
	if err != nil {
		panic(err)
	}
	m := hashToPoint(dm, mBuf)

	kdf := hkdf.New(sha512.New512_256, m.Marshal(), nil, []byte("Secret"))
	key = make([]byte, 32)
	_, err = kdf.Read(key)

	// calculate two enrollment points
	t0 := c0.Add(hc0.ScalarMultInt(c.clientPrivateKey))
	t1 := c1.Add(hc1.ScalarMultInt(c.clientPrivateKey)).Add(m.ScalarMultInt(c.clientPrivateKey))

	rec = &EnrollmentRecord{
		NS: resp.NS,
		NC: nc,
		T0: t0.Marshal(),
		T1: t1.Marshal(),
	}

	return
}

func (c *Client) validateProofOfSuccess(proof *ProofOfSuccess, nonce []byte, c0 *Point, c1 *Point, c0b, c1b []byte) bool {

	term1, term2, term3, blindX, err := proof.parse()

	if err != nil {
		return false
	}

	hs0 := hashToPoint(dhs0, nonce)
	hs1 := hashToPoint(dhs1, nonce)

	challenge := hashZ(proofOk, c.serverPublicKeyBytes, curveG.Marshal(), c0b, c1b, proof.Term1, proof.Term2, proof.Term3)

	//if term1 * (c0 ** challenge) != hs0 ** blind_x:
	// return False

	t1 := term1.Add(c0.ScalarMultInt(challenge))
	t2 := hs0.ScalarMultInt(blindX)

	if !t1.Equal(t2) {
		return false
	}

	// if term2 * (c1 ** challenge) != hs1 ** blind_x:
	// return False

	t1 = term2.Add(c1.ScalarMultInt(challenge))
	t2 = hs1.ScalarMultInt(blindX)

	if !t1.Equal(t2) {
		return false
	}

	//if term3 * (self.X ** challenge) != self.G ** blind_x:
	// return False

	t1 = term3.Add(c.serverPublicKey.ScalarMultInt(challenge))
	t2 = new(Point).ScalarBaseMultInt(blindX)

	if !t1.Equal(t2) {
		return false
	}

	return true
}

//CreateVerifyPasswordRequest creates a request in a form of elliptic curve point which is then need to be validated at the server side
func (c *Client) CreateVerifyPasswordRequest(password []byte, rec *EnrollmentRecord) (req *VerifyPasswordRequest, err error) {

	if rec == nil || len(rec.NC) == 0 || len(rec.NS) == 0 || len(rec.T0) == 0 {
		return nil, errors.New("invalid client record")
	}

	hc0 := hashToPoint(dhc0, rec.NC, password)
	minusY := gf.Neg(c.clientPrivateKey)

	t0, err := PointUnmarshal(rec.T0)
	if err != nil {
		return nil, errors.New("invalid proof")
	}

	c0 := t0.Add(hc0.ScalarMultInt(minusY))
	req = &VerifyPasswordRequest{
		C0: c0.Marshal(),
		NS: rec.NS,
	}
	return
}

// CheckResponseAndDecrypt verifies server's answer and extracts data encryption key on success
func (c *Client) CheckResponseAndDecrypt(password []byte, rec *EnrollmentRecord, resp *VerifyPasswordResponse) (key []byte, err error) {

	if resp == nil {
		return nil, errors.New("invalid response")
	}

	t0, t1, err := rec.parse()
	if err != nil {
		return nil, errors.New("invalid record")
	}

	c1, err := PointUnmarshal(resp.C1)
	if err != nil {
		return nil, err
	}

	hc0 := hashToPoint(dhc0, rec.NC, password)
	hc1 := hashToPoint(dhc1, rec.NC, password)

	//c0 = t0 * (hc0 ** (-self.y))

	minusY := gf.Neg(c.clientPrivateKey)

	c0 := t0.Add(hc0.ScalarMultInt(minusY))

	if resp.Res {

		if !c.validateProofOfSuccess(resp.ProofSuccess, rec.NS, c0, c1, c0.Marshal(), resp.C1) {
			return nil, errors.New("result is ok but proof is invalid")
		}

		//return ((t1 * (c1 ** (-1))) * (hc1 ** (-self.y))) ** (self.y ** (-1))

		m := (t1.Add(c1.Neg()).Add(hc1.ScalarMultInt(minusY))).ScalarMultInt(gf.Inv(c.clientPrivateKey))

		kdf := hkdf.New(sha512.New512_256, m.Marshal(), nil, []byte("Secret"))
		key = make([]byte, 32)
		_, err = kdf.Read(key)

		return

	}

	hs0 := hashToPoint(dhs0, rec.NS)
	err = c.validateProofOfFail(resp, c0, c1, hs0, hc0, hc1)

	return nil, err
}

func (c *Client) validateProofOfFail(resp *VerifyPasswordResponse, c0, c1, hs0, hc0, hc1 *Point) error {
	term1, term2, term3, term4, blindA, blindB, err := resp.ProofFail.parse()
	if err != nil {
		return errors.New("invalid public key")
	}

	challenge := hashZ(proofError, c.serverPublicKeyBytes, curveG.Marshal(), c0.Marshal(), resp.C1, resp.ProofFail.Term1, resp.ProofFail.Term2, resp.ProofFail.Term3, resp.ProofFail.Term4)
	//if term1 * term2 * (c1 ** challenge) != (c0 ** blind_a) * (hs0 ** blind_b):
	//return False
	//
	//if term3 * term4 * (I ** challenge) != (self.X ** blind_a) * (self.G ** blind_b):
	//return False

	t1 := term1.Add(term2).Add(c1.ScalarMultInt(challenge))
	t2 := c0.ScalarMultInt(blindA).Add(hs0.ScalarMultInt(blindB))

	if !t1.Equal(t2) {
		return errors.New("proof verification failed")
	}

	t1 = term3.Add(term4)
	t2 = c.serverPublicKey.ScalarMultInt(blindA).Add(new(Point).ScalarBaseMultInt(blindB))

	if !t1.Equal(t2) {
		return errors.New("verification failed")
	}
	return nil
}

// Rotate updates client's secret key and server's public key with server's update token
func (c *Client) Rotate(token *UpdateToken) error {

	a, b, err := token.parse()
	if err != nil {
		return err
	}

	c.clientPrivateKey = gf.Mul(c.clientPrivateKey, a)
	c.clientPrivateKeyBytes = c.clientPrivateKey.Bytes()

	pub := c.serverPublicKey.ScalarMultInt(a).Add(new(Point).ScalarBaseMultInt(b))

	c.serverPublicKey = pub
	c.serverPublicKeyBytes = pub.Marshal()
	return nil
}

// UpdateRecord needs to be applied to every database record to correspond to new private and public keys
func UpdateRecord(rec *EnrollmentRecord, token *UpdateToken) (updRec *EnrollmentRecord, err error) {

	a, b, err := token.parse()
	if err != nil {
		return nil, err
	}

	t0, t1, err := rec.parse()
	if err != nil {
		return nil, err
	}

	hs0 := hashToPoint(dhs0, rec.NS)
	hs1 := hashToPoint(dhs1, rec.NS)

	t00 := t0.ScalarMultInt(a).Add(hs0.ScalarMultInt(b))
	t11 := t1.ScalarMultInt(a).Add(hs1.ScalarMultInt(b))

	updRec = &EnrollmentRecord{
		T0: t00.Marshal(),
		T1: t11.Marshal(),
		NS: rec.NS,
		NC: rec.NC,
	}
	return
}

// RotateClientKeys returns a new pair of keys given old keys and an update token
func RotateClientKeys(clientPrivate, serverPublic []byte, token *UpdateToken) (newClientPrivate, newServerPublic []byte, err error) {
	a, b, err := token.parse()
	if err != nil {
		return
	}

	pub, err := PointUnmarshal(serverPublic)

	if err != nil {
		return
	}

	if len(clientPrivate) == 0 {
		err = errors.New("invalid private key")
		return
	}

	newClientPrivate = gf.MulBytes(clientPrivate, a).Bytes()
	pub = pub.ScalarMultInt(a).Add(new(Point).ScalarBaseMultInt(b))
	newServerPublic = pub.Marshal()
	return
}
