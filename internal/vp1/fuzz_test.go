package vp1

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// Повреждённая длина не должна обходить границы буфера или съедать следующий
// кадр. Проверяем оба пути чтения на одних и тех же данных с короткими чтениями.
func FuzzReadFrame(f *testing.F) {
	for _, seed := range [][]byte{{}, {0}, {0, 0}, {0, 1, 42}, {255, 255}, {0, 3, 1, 2}} {
		f.Add(seed, uint16(1024))
	}
	f.Fuzz(func(t *testing.T, wire []byte, limit uint16) {
		if len(wire) > 65537 {
			t.Skip()
		}
		maxBody := max(2, int(limit))
		r1, r2 := bytes.NewReader(wire), bytes.NewReader(wire)
		body1, err1 := readFrame(r1, maxBody)
		body2, err2 := readFrameInto(&oneByteReader{r: r2}, make([]byte, maxBody))
		if (err1 == nil) != (err2 == nil) {
			t.Fatalf("пути чтения расходятся: %v / %v", err1, err2)
		}
		if err1 != nil {
			return
		}
		if len(body1) == 0 || len(body1) > maxBody || !bytes.Equal(body1, body2) || r1.Len() != r2.Len() {
			t.Fatal("нарушены размер, содержимое или граница кадра")
		}
		var rebuilt bytes.Buffer
		if err := writeFrame(&rebuilt, body1); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(rebuilt.Bytes(), wire[:len(wire)-r1.Len()]) {
			t.Fatal("прочитанный кадр изменился при записи")
		}
	})
}

func FuzzUnpackPadded(f *testing.F) {
	for _, seed := range [][]byte{{}, {0}, {0, 0}, {0, 1, 42, 0}, {255, 255}} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > MaxPlaintext {
			t.Skip()
		}
		payload, err := unpackPadded(raw)
		if err != nil {
			return
		}
		if len(payload) > len(raw)-frameHeaderLen {
			t.Fatal("содержимое вышло за пределы кадра")
		}
		// Добивка не должна попадать в приложение, независимо от её содержимого.
		copyRaw := bytes.Clone(raw)
		for i := frameHeaderLen + len(payload); i < len(copyRaw); i++ {
			copyRaw[i] ^= 0xff
		}
		again, err := unpackPadded(copyRaw)
		if err != nil || !bytes.Equal(payload, again) {
			t.Fatal("изменение добивки изменило полезные данные")
		}
	})
}

func FuzzReadRequest(f *testing.F) {
	for _, seed := range [][]byte{{}, {1, 127, 0, 0, 1, 1, 187}, {3, 1, 'a', 0, 53}, {0x83, 1, 'a', 0, 53}, {3, 255}, {255}} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, wire []byte) {
		if len(wire) > 1024 {
			t.Skip()
		}
		r := bytes.NewReader(wire)
		addr, kind, err := ReadRequestOf(&oneByteReader{r: r})
		if err != nil {
			return
		}
		var encoded bytes.Buffer
		if err := WriteRequestOf(&encoded, addr, kind); err != nil {
			t.Fatalf("принятый адрес нельзя сериализовать: %v", err)
		}
		got, gotKind, err := ReadRequestOf(&encoded)
		if err != nil || got != addr || gotKind != kind || encoded.Len() != 0 {
			t.Fatal("тип запроса или адрес изменился при повторном разборе")
		}
	})
}

func FuzzReadDatagram(f *testing.F) {
	for _, seed := range [][]byte{{}, {0}, {0, 0}, {0, 1, 42}, {255, 255}, {0, 3, 1, 2}} {
		f.Add(seed, uint16(1500))
	}
	f.Fuzz(func(t *testing.T, wire []byte, capacity uint16) {
		if len(wire) > MaxDatagram+2 {
			t.Skip()
		}
		r := bytes.NewReader(wire)
		buf := make([]byte, int(capacity))
		n, err := ReadDatagram(&oneByteReader{r: r}, buf)
		if err != nil {
			return
		}
		if n < 0 || n > len(buf) || n != int(binary.BigEndian.Uint16(wire[:2])) || r.Len() != len(wire)-2-n {
			t.Fatal("нарушены размер датаграммы или граница чтения")
		}
		var encoded bytes.Buffer
		if err := WriteDatagram(&encoded, buf[:n]); err != nil || !bytes.Equal(encoded.Bytes(), wire[:2+n]) {
			t.Fatal("прочитанная датаграмма изменилась при записи")
		}
	})
}
