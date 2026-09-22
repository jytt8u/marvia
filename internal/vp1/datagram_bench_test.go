package vp1

import (
	"io"
	"testing"
)

// BenchmarkDatagramWrite — одна датаграмма QUIC размером с MTU: так идёт
// видео в TikTok и YouTube, тысячи в секунду.
func BenchmarkDatagramWrite(b *testing.B) {
	payload := make([]byte, 1350)
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := WriteDatagram(io.Discard, payload); err != nil {
			b.Fatal(err)
		}
	}
}
