//
// Copyright (c) 2024 Ted Unangst <tedu@tedunangst.com>
//
// Permission to use, copy, modify, and distribute this software for any
// purpose with or without fee is hereby granted, provided that the above
// copyright notice and this permission notice appear in all copies.
//
// THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
// WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
// MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
// ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
// WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
// ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
// OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.

package totp

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"time"
)

const secretSize = 20
const validInterval = 30
const codeDigits = 1000000

func NewSecret() string {
	secret := make([]byte, secretSize)
	rand.Read(secret)
	var buf bytes.Buffer
	enc := base32.NewEncoder(base32.StdEncoding, &buf)
	enc.Write(secret)
	enc.Close()
	return buf.String()
}

func GenerateCode(secret string) int {
	key, err := base32.StdEncoding.DecodeString(secret)
	if err != nil {
		return -1
	}
	now := time.Now().Unix()
	now /= validInterval
	return generateCodeCounter(key, now)
}

func generateCodeCounter(key []byte, when int64) int {
	mac := hmac.New(sha1.New, key)
	binary.Write(mac, binary.BigEndian, when)
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0xf
	code := ((int(sum[offset]) & 0x7f) << 24) |
		((int(sum[offset+1] & 0xff)) << 16) |
		((int(sum[offset+2] & 0xff)) << 8) |
		(int(sum[offset+3]) & 0xff)
	return code % codeDigits
}

func CheckCode(secret string, code int) bool {
	key, err := base32.StdEncoding.DecodeString(secret)
	if err != nil {
		return false
	}
	if code < 0 || code >= codeDigits {
		return false
	}
	now := time.Now().Unix()
	now /= validInterval
	c1 := generateCodeCounter(key, now-1)
	c2 := generateCodeCounter(key, now)
	okay := (code == c1) || (code == c2)
	return okay
}
