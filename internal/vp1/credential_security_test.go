package vp1

import (
	"bytes"
	"net"
	"testing"
)

// Утечка записи из списка доступа не должна превращать публичный ключ в
// пароль: для входа нужно доказать владение соответствующим закрытым ключом.
func TestPublicClientKeyAloneCannotAuthenticate(t *testing.T) {
	generate := func() KeyPair {
		t.Helper()
		pair, err := GenerateKeyPair()
		if err != nil {
			t.Fatal(err)
		}
		return pair
	}
	node, legitimate, impostor := generate(), generate(), generate()
	impostor.Public = bytes.Clone(legitimate.Public)
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	done := make(chan error, 1)
	go func() {
		defer server.Close()
		_, _, err := ServerHandshake(server, node, NewReplayGuard(ClockSkew), func(pub []byte) error {
			if !bytes.Equal(pub, legitimate.Public) {
				return ErrUnauthorized
			}
			return nil
		})
		done <- err
	}()
	_, clientErr := ClientHandshake(client, impostor, node.Public)
	serverErr := <-done
	if clientErr == nil || serverErr == nil {
		t.Fatalf("вход с чужим закрытым ключом принят: клиент=%v, сервер=%v", clientErr, serverErr)
	}
}
