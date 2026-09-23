//go:build ignore

// Тестовый сервер VLESS для проверки клиента на эмуляторе: слушает порт,
// пускает один UUID и выходит в сеть напрямую. Только для ручной проверки —
// в сборки не входит (go:build ignore).
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"

	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf/serial"

	_ "github.com/xtls/xray-core/app/dispatcher"
	_ "github.com/xtls/xray-core/app/proxyman/inbound"
	_ "github.com/xtls/xray-core/app/proxyman/outbound"
	_ "github.com/xtls/xray-core/proxy/freedom"
	_ "github.com/xtls/xray-core/proxy/vless/inbound"
	_ "github.com/xtls/xray-core/transport/internet/tcp"
	_ "github.com/xtls/xray-core/transport/internet/udp"
	_ "github.com/xtls/xray-core/transport/internet/websocket"
)

func main() {
	const uuid = "b831381d-6324-4d53-ad4f-8cda48b30811"
	raw, _ := json.Marshal(map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"inbounds": []any{map[string]any{"port": 18443, "listen": "127.0.0.1", "protocol": "vless",
			"settings":       map[string]any{"clients": []any{map[string]any{"id": uuid}}, "decryption": "none"},
			"streamSettings": map[string]any{"network": "ws", "wsSettings": map[string]any{"path": "/v"}}}},
		"outbounds": []any{map[string]any{"protocol": "freedom"}},
	})
	pb, err := serial.LoadJSONConfig(bytes.NewReader(raw))
	if err != nil {
		panic(err)
	}
	inst, err := core.New(pb)
	if err != nil {
		panic(err)
	}
	if err := inst.Start(); err != nil {
		panic(err)
	}
	fmt.Println("vless://" + uuid + "@10.0.2.2:18443?type=ws&path=%2Fv&security=none#Test%20VLESS")
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	<-c
}
