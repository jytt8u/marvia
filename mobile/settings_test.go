package mobile

import "testing"

// TestBrokenSettingsDoNotStopTheTunnel: сломанная строка настроек означает
// умолчания, а не отказ подключаться; незнакомое поле — не ошибка.
func TestBrokenSettingsDoNotStopTheTunnel(t *testing.T) {
	if got := parseSettings("{не json"); got != (tunnelSettings{}) {
		t.Fatalf("сломанные настройки дали %+v", got)
	}
	got := parseSettings(`{"fragment":true,"no_ipv6":true,"disable_reports":true,"из_будущего":1}`)
	if !got.Fragment || !got.NoIPv6 || !got.DisableReports {
		t.Fatalf("настройки не прочитались: %+v", got)
	}
	if !got.dial().Fragment {
		t.Fatal("дробление не дошло до дозвона нод")
	}
}
