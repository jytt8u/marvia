package main

import "testing"

func TestEveryModeDeliversTheSamePayloadAndEcho(t *testing.T) {
	s, err := newSetup()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			result, err := s.run(mode, 1<<20, 100)
			if err != nil {
				t.Fatal(err)
			}
			if result.Bytes != 1<<20 || result.MBps <= 0 || result.Seconds <= 0 || result.RTTMeanUS <= 0 {
				t.Fatalf("неполный результат: %+v", result)
			}
			if mode != "TCP" && result.Cipher == "" {
				t.Fatal("TLS-режим не подтвердил выбранный шифр")
			}
		})
	}
}
