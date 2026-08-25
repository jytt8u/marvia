package panel_test

import (
	"net/http"
	"sync"
	"testing"
	"time"
)

// userResponse — что панель отвечает про подписчика.
type userResponse struct {
	Created bool `json:"created"`
	User    struct {
		ID         int64      `json:"id"`
		Label      string     `json:"label"`
		ExternalID string     `json:"external_id"`
		ExpiresAt  *time.Time `json:"expires_at"`
	} `json:"user"`
	Issued []struct {
		Kind   string `json:"kind"`
		Secret string `json:"secret"`
	} `json:"issued"`
}

type usersResponse struct {
	Users []struct {
		ID         int64  `json:"id"`
		ExternalID string `json:"external_id"`
	} `json:"users"`
}

// TestRepeatedSaleGivesNothingTwice — повторное уведомление об оплате не
// заводит второго подписчика.
//
// Платёжные системы повторяют уведомление, когда бот не ответил. Бот мог
// упасть ровно между вызовом панели и записью у себя — и на повторе, без этой
// защиты, выдал бы покупателю второй доступ за ту же оплату. Продавец потерял
// бы месяц выручки и узнал бы об этом нескоро.
func TestRepeatedSaleGivesNothingTwice(t *testing.T) {
	h := newHarness(t)

	body := map[string]any{
		"label":       "заказ 1043",
		"external_id": "tg:584930221",
		"kinds":       []string{"vp1", "vless"},
	}

	var first userResponse
	if code := h.do(http.MethodPost, "/api/v1/users", adminToken, body, &first); code != http.StatusOK {
		t.Fatalf("первая продажа: код %d", code)
	}
	if !first.Created {
		t.Error("первая продажа отмечена как повтор")
	}
	if len(first.Issued) != 2 {
		t.Fatalf("выдано наборов доступа: %d, ожидалось 2", len(first.Issued))
	}

	var second userResponse
	if code := h.do(http.MethodPost, "/api/v1/users", adminToken, body, &second); code != http.StatusOK {
		t.Fatalf("повтор должен отвечать успехом, а вернул %d", code)
	}
	if second.Created {
		t.Error("повтор отмечен как новая продажа")
	}
	if second.User.ID != first.User.ID {
		t.Errorf("повтор завёл второго подписчика: id %d вместо %d", second.User.ID, first.User.ID)
	}
	if len(second.Issued) != 0 {
		t.Errorf("на повторе выдано %d новых секретов, а должно быть ноль", len(second.Issued))
	}

	var all usersResponse
	h.do(http.MethodGet, "/api/v1/users", adminToken, nil, &all)
	if len(all.Users) != 1 {
		t.Errorf("подписчиков в панели: %d, ожидался один", len(all.Users))
	}
}

// TestLookupByExternalID — бот находит покупателя по своему ключу.
//
// Без этого ему пришлось бы держать вторую базу соответствий telegram id →
// номер в панели, и при её потере связь покупателей с их подписками пропала бы.
func TestLookupByExternalID(t *testing.T) {
	h := newHarness(t)

	h.do(http.MethodPost, "/api/v1/users", adminToken, map[string]any{"external_id": "tg:1"}, nil)
	h.do(http.MethodPost, "/api/v1/users", adminToken, map[string]any{"external_id": "tg:2"}, nil)

	var found usersResponse
	if code := h.do(http.MethodGet, "/api/v1/users?external_id=tg:2", adminToken, nil, &found); code != http.StatusOK {
		t.Fatalf("поиск: код %d", code)
	}
	if len(found.Users) != 1 || found.Users[0].ExternalID != "tg:2" {
		t.Fatalf("нашлось не то: %+v", found.Users)
	}

	if code := h.do(http.MethodGet, "/api/v1/users?external_id=tg:нет-такого", adminToken, nil, nil); code != http.StatusNotFound {
		t.Errorf("на несуществующий ключ вернулся код %d, ожидался 404", code)
	}
}

// TestExtendKeepsRemainingDays — продление не съедает остаток.
//
// Покупатель, продливший за неделю до конца, эту неделю терять не должен.
// Через абсолютный expires_at бот легко ошибётся именно так: посчитает
// «сейчас плюс месяц» и молча отнимет остаток.
func TestExtendKeepsRemainingDays(t *testing.T) {
	h := newHarness(t)

	var created userResponse
	h.do(http.MethodPost, "/api/v1/users", adminToken,
		map[string]any{"label": "продлевает заранее", "expires_at": "40d"}, &created)

	var extended userResponse
	code := h.do(http.MethodPatch, "/api/v1/users/1", adminToken,
		map[string]any{"extend_by": "30d"}, &extended)
	if code != http.StatusOK {
		t.Fatalf("продление: код %d", code)
	}
	if extended.User.ExpiresAt == nil {
		t.Fatal("после продления нет срока")
	}

	left := time.Until(*extended.User.ExpiresAt)
	if left < 69*24*time.Hour || left > 71*24*time.Hour {
		t.Errorf("после продления осталось %.0f дней, ожидалось около 70", left.Hours()/24)
	}
}

// TestExtendFromNowWhenExpired — просроченную подписку продлеваем от сегодня.
//
// Иначе покупатель, вернувшийся через полгода, заплатил бы за месяц и получил
// подписку, истёкшую пять месяцев назад.
func TestExtendFromNowWhenExpired(t *testing.T) {
	h := newHarness(t)

	past := time.Now().UTC().Add(-90 * 24 * time.Hour).Format(time.RFC3339)
	h.do(http.MethodPost, "/api/v1/users", adminToken,
		map[string]any{"label": "вернулся", "expires_at": past}, nil)

	var extended userResponse
	if code := h.do(http.MethodPatch, "/api/v1/users/1", adminToken,
		map[string]any{"extend_by": "30d"}, &extended); code != http.StatusOK {
		t.Fatalf("продление: код %d", code)
	}

	left := time.Until(*extended.User.ExpiresAt)
	if left < 29*24*time.Hour || left > 31*24*time.Hour {
		t.Errorf("после продления осталось %.0f дней, ожидалось около 30", left.Hours()/24)
	}
}

// TestConcurrentExtensionsBothCount — два платежа сразу дают два месяца.
//
// Это и есть причина, по которой продление считается одним запросом к базе, а
// не тремя шагами «прочитать, посчитать, записать». При трёх шагах оба платежа
// прочитали бы один и тот же срок, записали бы одно и то же значение — и один
// месяц пропал бы бесследно.
func TestConcurrentExtensionsBothCount(t *testing.T) {
	h := newHarness(t)

	h.do(http.MethodPost, "/api/v1/users", adminToken, map[string]any{"label": "двойная оплата"}, nil)

	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.do(http.MethodPatch, "/api/v1/users/1", adminToken, map[string]any{"extend_by": "30d"}, nil)
		}()
	}
	wg.Wait()

	var user userResponse
	if code := h.do(http.MethodGet, "/api/v1/users/1", adminToken, nil, &user); code != http.StatusOK {
		t.Fatalf("чтение подписчика: код %d", code)
	}
	if user.User.ExpiresAt == nil {
		t.Fatal("срока нет вовсе")
	}

	left := time.Until(*user.User.ExpiresAt)
	if left < 59*24*time.Hour {
		t.Errorf("после двух продлений осталось %.0f дней, ожидалось около 60 — одна оплата пропала",
			left.Hours()/24)
	}
}

// TestExtendRefusesAmbiguousRequest — «продлить» и «назначить срок» вместе
// бессмысленны, и молча выбирать одно из двух панель не станет.
func TestExtendRefusesAmbiguousRequest(t *testing.T) {
	h := newHarness(t)
	h.do(http.MethodPost, "/api/v1/users", adminToken, map[string]any{"label": "х"}, nil)

	code := h.do(http.MethodPatch, "/api/v1/users/1", adminToken,
		map[string]any{"extend_by": "30d", "expires_at": "10d"}, nil)
	if code != http.StatusBadRequest {
		t.Errorf("противоречивый запрос принят с кодом %d, ожидался 400", code)
	}
}
