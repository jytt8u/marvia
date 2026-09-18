//go:build windows

package main

import (
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/client"
)

func TestDisplayedPingNeverUsesSetupTime(t *testing.T) {
	node := client.Node{ID: 1}
	m := client.Measurement{Node: node, Latency: 600 * time.Millisecond, Connect: 100 * time.Millisecond}
	if got := latencyOf([]client.Measurement{m}, node); got != 0 {
		t.Fatalf("без замера туннеля показан пинг %v", got)
	}
	m.RTT = 35 * time.Millisecond
	rows := nodeViews(&client.Supervisor{}, []client.Node{node}, []client.Measurement{m})
	if rows[0].MS != 35 || rows[0].SetupMS != 600 {
		t.Fatalf("замеры смешаны: %+v", rows[0])
	}
}
