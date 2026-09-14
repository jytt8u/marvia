package client

import "testing"

// Обновление предлагается только вперёд и только когда есть с чем сравнить.
func TestUpdateIsOfferedOnlyForward(t *testing.T) {
	sub := Subscription{Apps: map[string]AppOffer{
		"windows": {URL: "https://p/sub/x/app/windows", Version: "v0.10.0"},
		"android": {URL: "https://p/sub/x/app/android"},
	}}

	if _, ok := sub.Update("windows", "0.9.3"); !ok {
		t.Fatal("0.9.3 → v0.10.0: обновление не предложено")
	}
	if _, ok := sub.Update("windows", "0.10.0"); ok {
		t.Fatal("та же версия предложена как новая")
	}
	if _, ok := sub.Update("windows", "0.11.0"); ok {
		t.Fatal("старая сборка на панели предложена как новая")
	}
	if _, ok := sub.Update("windows", "dev"); ok {
		t.Fatal("сборке разработчика предложено обновление")
	}
	if _, ok := sub.Update("android", "0.1"); ok {
		t.Fatal("без версии на панели сравнивать не с чем, а обновление предложено")
	}
	if _, ok := sub.Update("linux", "0.1"); ok {
		t.Fatal("платформа, которой нет на панели, получила обновление")
	}
	if _, ok := sub.Update("windows", "что-то"); ok {
		t.Fatal("непонятная своя версия сочтена устаревшей")
	}
}
