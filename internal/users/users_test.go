package users

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/veilproject/veil/internal/vp1"
)

func newKey(t *testing.T) (raw []byte, encoded string) {
	t.Helper()
	pair, err := vp1.GenerateKeyPair()
	if err != nil {
		t.Fatalf("генерация ключа: %v", err)
	}
	return pair.Public, vp1.EncodeKey(pair.Public)
}

func addr(ip string) net.Addr {
	return &net.TCPAddr{IP: net.ParseIP(ip), Port: 40000}
}

func TestAdmitUnknownKey(t *testing.T) {
	_, encoded := newKey(t)
	other, _ := newKey(t)

	r, err := NewRegistry([]User{{PublicKey: encoded, Enabled: true}})
	if err != nil {
		t.Fatalf("реестр: %v", err)
	}

	if _, err := r.Admit(other, addr("1.2.3.4")); !errors.Is(err, ErrUnknown) {
		t.Fatalf("ожидался ErrUnknown, получено: %v", err)
	}
}

func TestAdmitDisabledAndExpired(t *testing.T) {
	rawOff, encOff := newKey(t)
	rawOld, encOld := newKey(t)

	r, err := NewRegistry([]User{
		{PublicKey: encOff, Enabled: false},
		{PublicKey: encOld, Enabled: true, ExpiresAt: time.Now().Add(-time.Hour)},
	})
	if err != nil {
		t.Fatalf("реестр: %v", err)
	}

	if _, err := r.Admit(rawOff, addr("1.2.3.4")); !errors.Is(err, ErrDisabled) {
		t.Fatalf("выключенный пользователь: ожидался ErrDisabled, получено: %v", err)
	}
	if _, err := r.Admit(rawOld, addr("1.2.3.4")); !errors.Is(err, ErrExpired) {
		t.Fatalf("истёкшая подписка: ожидался ErrExpired, получено: %v", err)
	}
}

// TestQuotaBlocksMidSession: квота должна срабатывать не только на входе, но и
// посреди сессии. Иначе пользователь, подключившийся с нулевым расходом,
// качает сколько угодно до следующего переподключения.
func TestQuotaBlocksMidSession(t *testing.T) {
	raw, encoded := newKey(t)

	r, err := NewRegistry([]User{{PublicKey: encoded, Enabled: true, TrafficLimit: 1000}})
	if err != nil {
		t.Fatalf("реестр: %v", err)
	}

	session, err := r.Admit(raw, addr("1.2.3.4"))
	if err != nil {
		t.Fatalf("первое подключение должно пройти: %v", err)
	}

	if over := session.Add(300, 300); over {
		t.Fatal("600 байт из 1000 — квота не должна быть исчерпана")
	}
	if over := session.Add(200, 200); !over {
		t.Fatal("1000 байт из 1000 — квота должна быть исчерпана")
	}
	session.Close()

	if _, err := r.Admit(raw, addr("1.2.3.4")); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("после исчерпания квоты: ожидался ErrQuotaExceeded, получено: %v", err)
	}
}

// TestIPLimit — ровно та проверка, ради которой продавцы и берут панели:
// один купил, раздал конфиг друзьям.
func TestIPLimit(t *testing.T) {
	raw, encoded := newKey(t)

	r, err := NewRegistry([]User{{PublicKey: encoded, Enabled: true, MaxIPs: 2}})
	if err != nil {
		t.Fatalf("реестр: %v", err)
	}

	for _, ip := range []string{"10.0.0.1", "10.0.0.2"} {
		s, err := r.Admit(raw, addr(ip))
		if err != nil {
			t.Fatalf("адрес %s должен пройти: %v", ip, err)
		}
		s.Close()
	}

	// Тот же адрес снова — можно: это то же устройство.
	s, err := r.Admit(raw, addr("10.0.0.1"))
	if err != nil {
		t.Fatalf("повторное подключение с известного адреса должно проходить: %v", err)
	}
	s.Close()

	if _, err := r.Admit(raw, addr("10.0.0.3")); !errors.Is(err, ErrTooManyIPs) {
		t.Fatalf("третий адрес: ожидался ErrTooManyIPs, получено: %v", err)
	}
}

// TestIPWindowExpires: человек, переехавший с домашнего Wi-Fi на мобильную
// сеть, не должен упираться в лимит устройств навсегда.
func TestIPWindowExpires(t *testing.T) {
	raw, encoded := newKey(t)

	r, err := NewRegistry([]User{{PublicKey: encoded, Enabled: true, MaxIPs: 1}})
	if err != nil {
		t.Fatalf("реестр: %v", err)
	}

	now := time.Now()
	r.now = func() time.Time { return now }

	s, err := r.Admit(raw, addr("10.0.0.1"))
	if err != nil {
		t.Fatalf("первое подключение: %v", err)
	}
	s.Close()

	if _, err := r.Admit(raw, addr("10.0.0.2")); !errors.Is(err, ErrTooManyIPs) {
		t.Fatalf("второй адрес сразу: ожидался ErrTooManyIPs, получено: %v", err)
	}

	now = now.Add(ipWindow + time.Minute)
	s, err = r.Admit(raw, addr("10.0.0.2"))
	if err != nil {
		t.Fatalf("после окна адрес должен пройти: %v", err)
	}
	s.Close()
}

func TestConnLimit(t *testing.T) {
	raw, encoded := newKey(t)

	r, err := NewRegistry([]User{{PublicKey: encoded, Enabled: true, MaxConns: 2}})
	if err != nil {
		t.Fatalf("реестр: %v", err)
	}

	first, err := r.Admit(raw, addr("1.2.3.4"))
	if err != nil {
		t.Fatalf("первое соединение: %v", err)
	}
	second, err := r.Admit(raw, addr("1.2.3.4"))
	if err != nil {
		t.Fatalf("второе соединение: %v", err)
	}
	if _, err := r.Admit(raw, addr("1.2.3.4")); !errors.Is(err, ErrTooManyConns) {
		t.Fatalf("третье соединение: ожидался ErrTooManyConns, получено: %v", err)
	}

	first.Close()
	third, err := r.Admit(raw, addr("1.2.3.4"))
	if err != nil {
		t.Fatalf("после освобождения слота: %v", err)
	}
	third.Close()
	second.Close()
}

// TestReplaceKeepsUsage: перечитывание файла пользователей не должно обнулять
// расход — иначе достаточно тронуть конфиг, чтобы всем начислилось заново.
func TestReplaceKeepsUsage(t *testing.T) {
	raw, encoded := newKey(t)

	r, err := NewRegistry([]User{{PublicKey: encoded, Enabled: true, TrafficLimit: 1000}})
	if err != nil {
		t.Fatalf("реестр: %v", err)
	}

	session, err := r.Admit(raw, addr("1.2.3.4"))
	if err != nil {
		t.Fatalf("подключение: %v", err)
	}
	session.Add(400, 100)

	// Владелец ноды поднял лимит и переписал файл.
	if err := r.Replace([]User{{PublicKey: encoded, Enabled: true, TrafficLimit: 5000}}); err != nil {
		t.Fatalf("замена списка: %v", err)
	}

	stats := r.Stats()
	if len(stats) != 1 {
		t.Fatalf("ожидался один пользователь, получено %d", len(stats))
	}
	if stats[0].Usage.Total() != 500 {
		t.Fatalf("расход после перечитывания %d, ожидалось 500", stats[0].Usage.Total())
	}
	if stats[0].Conns != 1 {
		t.Fatalf("живые соединения потерялись: %d", stats[0].Conns)
	}
	session.Close()
}

func TestLoadUsersJSON(t *testing.T) {
	_, encoded := newKey(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "users.json")

	content := `{
  "users": [
    {
      "public_key": "` + encoded + `",
      "label": "телефон Пети",
      "enabled": true,
      "traffic_limit": 107374182400,
      "max_ips": 3
    }
  ]
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("запись файла: %v", err)
	}

	list, err := LoadUsers(path)
	if err != nil {
		t.Fatalf("чтение: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("получено %d пользователей", len(list))
	}
	if list[0].Label != "телефон Пети" || list[0].MaxIPs != 3 || list[0].TrafficLimit != 107374182400 {
		t.Fatalf("поля разобраны неверно: %+v", list[0])
	}
}

func TestLoadUsersPlainList(t *testing.T) {
	_, first := newKey(t)
	_, second := newKey(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "clients.txt")
	content := "# мои устройства\n" + first + "  # ноутбук\n\n" + second + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("запись файла: %v", err)
	}

	list, err := LoadUsers(path)
	if err != nil {
		t.Fatalf("чтение: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("получено %d пользователей, ожидалось 2", len(list))
	}
	if !list[0].Enabled || list[0].Label != "ноутбук" {
		t.Fatalf("первая запись разобрана неверно: %+v", list[0])
	}
	if list[0].TrafficLimit != 0 {
		t.Fatal("в простом формате лимитов быть не должно")
	}
}

// TestUsageSurvivesRestart: перезапуск ноды не должен обнулять расход, иначе
// месячную квоту можно сбросить, попросив продавца «перезагрузить сервер».
func TestUsageSurvivesRestart(t *testing.T) {
	raw, encoded := newKey(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "usage.json")

	list := []User{{PublicKey: encoded, Enabled: true, TrafficLimit: 1000}}

	before, err := NewRegistry(list)
	if err != nil {
		t.Fatalf("реестр: %v", err)
	}
	session, err := before.Admit(raw, addr("1.2.3.4"))
	if err != nil {
		t.Fatalf("подключение: %v", err)
	}
	session.Add(700, 100)
	session.Close()

	if err := before.SaveUsage(path); err != nil {
		t.Fatalf("сохранение расхода: %v", err)
	}

	// «Перезапуск»: новый реестр, тот же файл.
	after, err := NewRegistry(list)
	if err != nil {
		t.Fatalf("реестр после перезапуска: %v", err)
	}
	saved, err := LoadUsage(path)
	if err != nil {
		t.Fatalf("чтение расхода: %v", err)
	}
	after.RestoreUsage(saved)

	stats := after.Stats()
	if len(stats) != 1 || stats[0].Usage.Total() != 800 {
		t.Fatalf("расход после перезапуска: %+v, ожидалось 800", stats)
	}

	// Остаток квоты — 200 байт, значит пустить ещё можно.
	session, err = after.Admit(raw, addr("1.2.3.4"))
	if err != nil {
		t.Fatalf("остаток квоты есть, подключение должно пройти: %v", err)
	}
	if over := session.Add(200, 0); !over {
		t.Fatal("квота должна была исчерпаться")
	}
	session.Close()
}

func TestLoadUsageMissingFileIsNotAnError(t *testing.T) {
	usage, err := LoadUsage(filepath.Join(t.TempDir(), "нет-такого.json"))
	if err != nil {
		t.Fatalf("отсутствие файла не должно быть ошибкой: %v", err)
	}
	if len(usage) != 0 {
		t.Fatalf("ожидалась пустая карта, получено %d записей", len(usage))
	}
}

// TestAccountSharedBetweenKeys: телефон и ноутбук одного человека — разные
// ключи, но один аккаунт. Квота, срок и лимит адресов у них общие, иначе
// достаточно попросить второй конфиг, чтобы удвоить себе трафик.
func TestAccountSharedBetweenKeys(t *testing.T) {
	phoneRaw, phoneKey := newKey(t)
	laptopRaw, laptopKey := newKey(t)

	list := []User{
		{PublicKey: phoneKey, Label: "телефон", Enabled: true, TrafficLimit: 1000, Account: "42"},
		{PublicKey: laptopKey, Label: "ноутбук", Enabled: true, TrafficLimit: 1000, Account: "42"},
	}
	r, err := NewRegistry(list)
	if err != nil {
		t.Fatalf("реестр: %v", err)
	}
	if r.Len() != 1 {
		t.Fatalf("аккаунтов %d, ожидался 1", r.Len())
	}

	phone, err := r.Admit(phoneRaw, addr("10.0.0.1"))
	if err != nil {
		t.Fatalf("телефон: %v", err)
	}
	if over := phone.Add(600, 0); over {
		t.Fatal("600 из 1000 — квота ещё не исчерпана")
	}
	phone.Close()

	// Ноутбук должен видеть уже израсходованное телефоном.
	laptop, err := r.Admit(laptopRaw, addr("10.0.0.2"))
	if err != nil {
		t.Fatalf("ноутбук: %v", err)
	}
	if over := laptop.Add(400, 0); !over {
		t.Fatal("общая квота должна была исчерпаться на втором устройстве")
	}
	laptop.Close()

	if _, err := r.Admit(phoneRaw, addr("10.0.0.1")); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("телефон после исчерпания общей квоты: получено %v", err)
	}

	stats := r.Stats()
	if len(stats) != 1 || stats[0].Usage.Total() != 1000 {
		t.Fatalf("статистика должна быть общей на аккаунт: %+v", stats)
	}
}

// TestSessionInvalidatedMidFlight: отключение подписчика должно доходить до
// уже открытой сессии. Без этого «отключил за неоплату» ничего не меняет,
// пока человек сам не переподключится, — а зачем ему переподключаться.
func TestSessionInvalidatedMidFlight(t *testing.T) {
	raw, encoded := newKey(t)

	r, err := NewRegistry([]User{{PublicKey: encoded, Enabled: true, Account: "7"}})
	if err != nil {
		t.Fatalf("реестр: %v", err)
	}

	session, err := r.Admit(raw, addr("1.2.3.4"))
	if err != nil {
		t.Fatalf("подключение: %v", err)
	}
	if err := session.Valid(); err != nil {
		t.Fatalf("свежая сессия должна быть живой: %v", err)
	}

	// Продавец отключил подписчика, панель прислала новый список.
	if err := r.Replace([]User{{PublicKey: encoded, Enabled: false, Account: "7"}}); err != nil {
		t.Fatalf("замена списка: %v", err)
	}
	if err := session.Valid(); !errors.Is(err, ErrDisabled) {
		t.Fatalf("ожидался ErrDisabled, получено: %v", err)
	}

	// Истёкшая подписка — то же самое.
	if err := r.Replace([]User{{PublicKey: encoded, Enabled: true, Account: "7",
		ExpiresAt: time.Now().Add(-time.Minute)}}); err != nil {
		t.Fatalf("замена списка: %v", err)
	}
	if err := session.Valid(); !errors.Is(err, ErrExpired) {
		t.Fatalf("ожидался ErrExpired, получено: %v", err)
	}

	// Удалили из списка целиком.
	other, otherKey := newKey(t)
	_ = other
	if err := r.Replace([]User{{PublicKey: otherKey, Enabled: true}}); err != nil {
		t.Fatalf("замена списка: %v", err)
	}
	if err := session.Valid(); !errors.Is(err, ErrUnknown) {
		t.Fatalf("ожидался ErrUnknown, получено: %v", err)
	}
	session.Close()
}
