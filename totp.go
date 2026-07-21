package main

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

type TOTPResult struct {
	Code        string `json:"Code"`
	SecondsLeft int    `json:"SecondsLeft"`
}

func GenerateTOTP(secret string) (*TOTPResult, error) {
	secret = strings.ToUpper(strings.ReplaceAll(secret, " ", ""))
	if pad := len(secret) % 8; pad != 0 {
		secret += strings.Repeat("=", 8-pad)
	}
	key, err := base32.StdEncoding.DecodeString(secret)
	if err != nil {
		return nil, fmt.Errorf("TOTP 密钥无效: %w", err)
	}

	now := time.Now().Unix()
	counter := uint64(now / 30)
	secondsLeft := int(30 - now%30)

	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, counter)

	mac := hmac.New(sha1.New, key)
	mac.Write(buf)
	h := mac.Sum(nil)

	offset := h[len(h)-1] & 0x0f
	code := (int(h[offset]&0x7f)<<24 |
		int(h[offset+1])<<16 |
		int(h[offset+2])<<8 |
		int(h[offset+3])) % 1_000_000

	return &TOTPResult{
		Code:        fmt.Sprintf("%06d", code),
		SecondsLeft: secondsLeft,
	}, nil
}
