package us88

import (
	"bytes"
	"crypto/cipher"
	"crypto/des"
	"encoding/binary"
	"encoding/hex"
	"testing"
)

func decrypt(t *testing.T, h []byte) []byte {
	block, _ := des.NewCipher(key)
	out := make([]byte, 504)
	cipher.NewCBCDecrypter(block, key).CryptBlocks(out, h[:504])
	return out
}

func TestHeaderLayout(t *testing.T) {
	size := []byte{0, 1, 0xe2, 0x40}
	h := Header(cmdPushJPG, size, 123456)
	if len(h) != 512 || h[510] != 0xa1 || h[511] != 0x1a || !bytes.Equal(h[504:510], make([]byte, 6)) {
		t.Fatalf("bad framing: len %d trailer %x", len(h), h[504:])
	}
	p := decrypt(t, h)
	if p[0] != cmdPushJPG || p[2] != 0x1a || p[3] != 0x6d || binary.LittleEndian.Uint32(p[4:8]) != 123456 || binary.BigEndian.Uint32(p[8:12]) != 123456 {
		t.Fatalf("plaintext fields wrong: % x", p[:12])
	}
	if !bytes.Equal(p[500:], []byte{4, 4, 4, 4}) {
		t.Fatalf("PKCS7 padding wrong: % x", p[500:])
	}
}

// Known answer from the Node implementation that was verified on the panel (same cmd, params, timestamp).
const nodeVector = "3f1b23585dc8865e32b5195dd3c5e6f155d00d69915cc8db88b0f09fc3d1cea37969a2788fbc7107b907af1c3ca7544b577f7dadc07edd8264891657265de35aac1f73efa7000244f25a489411dd3abcc8574e8efb76b50d096f9fdfb27a5b24afbd3de8fbb24199f36d756ff9d4695d9b3a2ec0067aa1ac2ff96d2516159df04e01222a1f053282d228ccdb81c74a69080545a9d76df3b6504be0092bd05a70b96de6a46e91905d48b4515eeeacc8a06420922a8abab66e078673cd8cb1d8703df00e28b5a495b04d3999ff769b833781d74e76040f3fe15785844b54e0a344f7d48b6ee03be91601c086cc950cf0359c732a841bbb4b783b4e7d4b52749955c7083a1d1c4c850c7f2ca52ba902571cdcea1ab10519c2f6bb14385faf306e59b1f286af1af9567962fc9647d1cecbfa070b8eac1ea7dade16a4766b4fd0f8e9a94d516d0bb4e552b56caef10b2807a1c7849104a6eac8682e3febf9fda7dc8b55ccbfe1a28d4fd454199aa2aa29640f5eff64018f1deecc7b34856b310234b255419349abab7f5e69411ea70f9e61294e8c91b84153ab087e1b636583dac472c900534e55d60d0cdd63f41e6288a3b9a63ca756c828cbc911359552bebaf7bf20135231511717ea3682a1963407cb34f277b9dfa23aef8a3cd49aa3cf1d6448769ceebc70198982b2e06bbba21eb14e7b3f2bf72a8affaa000000000000a11a"

func TestHeaderMatchesVerifiedNodeImplementation(t *testing.T) {
	if got := hex.EncodeToString(Header(cmdPushJPG, []byte{0, 1, 0xe2, 0x40}, 123456)); got != nodeVector {
		t.Fatalf("header differs from the hardware-verified implementation:\n%s", got)
	}
}

func TestParamsCapped(t *testing.T) {
	if h := Header(cmdBrightness, bytes.Repeat([]byte{7}, 900), 1); len(h) != 512 {
		t.Fatal("oversized params changed header size")
	}
}
