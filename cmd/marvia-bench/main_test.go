package main

import "testing"

// Профиль снимается на том же пути, что и сравнение протоколов: получатель
// проверяет весь поток. Настройка соединения входит в профиль, но основное
// время занимает передача 64 МиБ; для итоговых цифр используется CLI-стенд.
func BenchmarkProtocolTransfer(b *testing.B) {
	s, err := newSetup()
	if err != nil {
		b.Fatal(err)
	}
	for _, mode := range modes {
		b.Run(mode, func(b *testing.B) {
			b.SetBytes(64 << 20)
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := s.run(mode, 64<<20, 1); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestEveryModeDeliversTheSamePayloadAndEcho(t *testing.T) {
	// Один МиБ на loopback иногда укладывается в один тик часов Windows,
	// из-за чего исправная передача давала нулевую длительность.
	const testBytes = 64 << 20
	s, err := newSetup()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			result, err := s.run(mode, testBytes, 100)
			if err != nil {
				t.Fatal(err)
			}
			if result.Bytes != testBytes || result.MBps <= 0 || result.Seconds <= 0 || result.RTTMeanUS <= 0 {
				t.Fatalf("неполный результат: %+v", result)
			}
			if mode != "TCP" && result.Cipher == "" {
				t.Fatal("TLS-режим не подтвердил выбранный шифр")
			}
		})
	}
}
