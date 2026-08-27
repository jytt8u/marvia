package bot

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/veilproject/veil/internal/panel"
)

// Бота проверяем против настоящей панели, а не против заглушки.
//
// Заглушка подтверждает только то, что бот шлёт запросы, которые мы сами и
// придумали. Настоящая панель отвечает так, как ответит у продавца: с теми же
// сроками, той же защитой от повторной оплаты и теми же ссылками.

// fakeTelegram — телеграм, который всё запоминает.
type fakeTelegram struct {
	srv *httptest.Server

	sent     []sentMessage
	invoices []sentInvoice
}

type sentMessage struct {
	Chat int64  `json:"chat_id"`
	Text string `json:"text"`
}

type sentInvoice struct {
	Chat    int64  `json:"chat_id"`
	Title   string `json:"title"`
	Payload string `json:"payload"`
	Prices  []struct {
		Amount int64 `json:"amount"`
	} `json:"prices"`
}

func newFakeTelegram(t *testing.T) *fakeTelegram {
	t.Helper()

	f := &fakeTelegram{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)

		switch {
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			var m sentMessage
			_ = json.Unmarshal(raw, &m)
			f.sent = append(f.sent, m)
		case strings.HasSuffix(r.URL.Path, "/sendInvoice"):
			var i sentInvoice
			_ = json.Unmarshal(raw, &i)
			f.invoices = append(f.invoices, i)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	t.Cleanup(f.srv.Close)

	return f
}

// texts склеивает всё сказанное покупателю.
func (f *fakeTelegram) texts() string {
	var b strings.Builder
	for _, m := range f.sent {
		b.WriteString(m.Text)
		b.WriteString("\n")
	}
	return b.String()
}

// testBot поднимает бота с настоящей панелью и поддельным телеграмом.
func testBot(t *testing.T, payment string) (*Bot, *fakeTelegram) {
	t.Helper()

	store, err := panel.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatalf("база: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	admin, err := panel.NewToken()
	if err != nil {
		t.Fatalf("токен: %v", err)
	}
	api := httptest.NewServer(panel.NewAPI(store, admin, "https://panel.example.test", t.TempDir()).Handler())
	t.Cleanup(api.Close)

	// Ключ с правом users — ровно тот, что получает бот у продавца.
	_, secret, err := store.CreateAPIKey(context.Background(), "бот", []string{panel.ScopeUsers})
	if err != nil {
		t.Fatalf("ключ бота: %v", err)
	}

	tg := newFakeTelegram(t)

	state, err := OpenState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("состояние: %v", err)
	}

	cfg := Config{
		Token:   "тест",
		Panel:   api.URL,
		Key:     secret,
		Admins:  []int64{999},
		Payment: payment,
		Tariffs: []Tariff{
			{ID: "trial", Title: "Пробные 3 дня", Days: 3, Price: 0, TrafficGB: 5, Devices: 1},
			{ID: "m1", Title: "1 месяц", Days: 30, Price: 150, TrafficGB: 100, Devices: 3},
		},
	}
	if err := cfg.check(); err != nil {
		t.Fatalf("настройки: %v", err)
	}

	b := New(cfg, state)
	b.tg.base = tg.srv.URL

	return b, tg
}

func buyer(id int64) *TGUser {
	return &TGUser{ID: id, FirstName: "Покупатель", Username: "buyer"}
}

// TestPaymentGivesAccess — за оплатой сразу идёт рабочая ссылка.
//
// Покупатель, заплативший и не получивший ничего, идёт не пробовать ещё раз,
// а писать продавцу. Поэтому ссылка обязана приходить в том же ответе.
func TestPaymentGivesAccess(t *testing.T) {
	b, tg := testBot(t, PayStars)

	b.paid(context.Background(), buyer(100), &SuccessfulPayment{
		Payload: "t:m1", ChargeID: "charge-1", Total: 150, Currency: "XTR",
	})

	all := tg.texts()
	if !strings.Contains(all, "veil-account://") {
		t.Fatalf("после оплаты не пришла ссылка доступа:\n%s", all)
	}
	if !strings.Contains(all, "/sub/") {
		t.Errorf("не пришла ссылка подписки для чужих клиентов:\n%s", all)
	}

	user, err := b.panel.ByExternalID(context.Background(), "tg:100")
	if err != nil {
		t.Fatalf("покупателя нет в панели: %v", err)
	}
	if user.TrafficLimit != 100*1024*1024*1024 {
		t.Errorf("квота не из тарифа: %d", user.TrafficLimit)
	}
	if user.ExpiresAt == nil || user.ExpiresAt.Before(time.Now().AddDate(0, 0, 29)) {
		t.Errorf("срок не на месяц: %v", user.ExpiresAt)
	}
}

// TestRepeatedPaymentDoesNotDouble — повтор уведомления не даёт второго срока.
//
// Телеграм повторяет уведомление, если бот не ответил, а бот может упасть ровно
// между списанием и ответом. Без защиты покупатель получал бы два месяца за
// одни деньги — и продавец узнавал бы об этом из статистики.
func TestRepeatedPaymentDoesNotDouble(t *testing.T) {
	b, _ := testBot(t, PayStars)
	ctx := context.Background()

	pay := &SuccessfulPayment{Payload: "t:m1", ChargeID: "charge-7"}

	b.paid(ctx, buyer(101), pay)
	first, err := b.panel.ByExternalID(ctx, "tg:101")
	if err != nil {
		t.Fatalf("покупателя нет: %v", err)
	}

	// Тот же платёж приходит ещё раз, и ещё раз.
	b.paid(ctx, buyer(101), pay)
	b.paid(ctx, buyer(101), pay)

	after, err := b.panel.ByExternalID(ctx, "tg:101")
	if err != nil {
		t.Fatalf("покупателя нет: %v", err)
	}
	if !after.ExpiresAt.Equal(*first.ExpiresAt) {
		t.Fatalf("срок вырос на повторе: было %v, стало %v", first.ExpiresAt, after.ExpiresAt)
	}
}

// TestSecondPurchaseExtends — вторая покупка прибавляется к остатку.
func TestSecondPurchaseExtends(t *testing.T) {
	b, _ := testBot(t, PayStars)
	ctx := context.Background()

	b.paid(ctx, buyer(102), &SuccessfulPayment{Payload: "t:m1", ChargeID: "первый"})
	first, _ := b.panel.ByExternalID(ctx, "tg:102")

	b.paid(ctx, buyer(102), &SuccessfulPayment{Payload: "t:m1", ChargeID: "второй"})
	second, _ := b.panel.ByExternalID(ctx, "tg:102")

	grew := second.ExpiresAt.Sub(*first.ExpiresAt)
	if grew < 29*24*time.Hour {
		t.Fatalf("вторая оплата не продлила: было %v, стало %v", first.ExpiresAt, second.ExpiresAt)
	}
}

// TestTrialOnlyOnce — бесплатный доступ не выдаётся дважды.
func TestTrialOnlyOnce(t *testing.T) {
	b, tg := testBot(t, PayStars)
	ctx := context.Background()

	b.wantsToBuy(ctx, 103, buyer(103), "trial")
	if _, err := b.panel.ByExternalID(ctx, "tg:103"); err != nil {
		t.Fatalf("пробный доступ не выдался: %v", err)
	}

	before := len(tg.sent)
	b.wantsToBuy(ctx, 103, buyer(103), "trial")

	last := tg.sent[len(tg.sent)-1].Text
	if !strings.Contains(last, "один раз") {
		t.Fatalf("второй пробный доступ не отказан: %q", last)
	}
	if len(tg.sent) <= before {
		t.Errorf("бот промолчал в ответ на повторный пробный")
	}
}

// TestPaidTariffAsksForMoney — платный тариф не выдаётся без оплаты.
//
// Проверка от опечатки в будущем: перепутанная ветка здесь означает раздачу
// доступа бесплатно, и заметит это продавец не сразу.
func TestPaidTariffAsksForMoney(t *testing.T) {
	b, tg := testBot(t, PayStars)

	b.wantsToBuy(context.Background(), 104, buyer(104), "m1")

	if _, err := b.panel.ByExternalID(context.Background(), "tg:104"); err == nil {
		t.Fatal("доступ выдан до оплаты")
	}
	if len(tg.invoices) != 1 {
		t.Fatalf("счёт не выставлен: %+v", tg.invoices)
	}
	if got := tg.invoices[0].Payload; got != "t:m1" {
		t.Errorf("в счёте не тот тариф: %q", got)
	}
	if len(tg.invoices[0].Prices) != 1 || tg.invoices[0].Prices[0].Amount != 150 {
		t.Errorf("в счёте не та цена: %+v", tg.invoices[0].Prices)
	}
}

// TestManualGrantNeedsAdmin — чужой не выдаёт себе доступ кнопкой продавца.
//
// callback_data подделывается тривиально: телеграм отдаёт её любому, кто нажал
// кнопку в пересланном сообщении.
func TestManualGrantNeedsAdmin(t *testing.T) {
	b, tg := testBot(t, PayManual)
	ctx := context.Background()

	b.grant(ctx, 105, buyer(105), "105.m1")

	if _, err := b.panel.ByExternalID(ctx, "tg:105"); err == nil {
		t.Fatal("посторонний выдал себе доступ кнопкой продавца")
	}
	if last := tg.sent[len(tg.sent)-1].Text; !strings.Contains(last, "не для тебя") {
		t.Errorf("отказа не было: %q", last)
	}

	// А продавец — выдаёт.
	b.grant(ctx, 999, &TGUser{ID: 999}, "105.m1")
	if _, err := b.panel.ByExternalID(ctx, "tg:105"); err != nil {
		t.Fatalf("продавец не смог выдать доступ: %v", err)
	}
}

// TestManualGrantTwiceIsOneMonth — два нажатия «Выдать» не дают двух сроков.
func TestManualGrantTwiceIsOneMonth(t *testing.T) {
	b, _ := testBot(t, PayManual)
	ctx := context.Background()
	admin := &TGUser{ID: 999}

	b.grant(ctx, 999, admin, "106.m1")
	first, err := b.panel.ByExternalID(ctx, "tg:106")
	if err != nil {
		t.Fatalf("доступ не выдан: %v", err)
	}

	b.grant(ctx, 999, admin, "106.m1")
	second, _ := b.panel.ByExternalID(ctx, "tg:106")

	if !second.ExpiresAt.Equal(*first.ExpiresAt) {
		t.Fatalf("повторное нажатие продлило: было %v, стало %v", first.ExpiresAt, second.ExpiresAt)
	}
}

// TestReminderOncePerTerm — напоминание приходит один раз на срок.
func TestReminderOncePerTerm(t *testing.T) {
	b, tg := testBot(t, PayStars)
	ctx := context.Background()

	// Покупатель, у которого срок кончается завтра.
	sale, err := b.panel.Sell(ctx, "tg:107", "кончается", Tariff{Days: 1})
	if err != nil {
		t.Fatalf("продажа: %v", err)
	}
	_ = sale

	b.remindOnce(ctx)
	first := len(tg.sent)
	if first == 0 {
		t.Fatal("напоминание не пришло")
	}
	if !strings.Contains(tg.texts(), "заканчивается") {
		t.Errorf("не то напоминание:\n%s", tg.texts())
	}

	b.remindOnce(ctx)
	if len(tg.sent) != first {
		t.Fatalf("напомнило второй раз: было %d сообщений, стало %d", first, len(tg.sent))
	}
}

// TestReminderSkipsStrangers — тем, кто заведён не ботом, бот не пишет.
func TestReminderSkipsStrangers(t *testing.T) {
	b, tg := testBot(t, PayStars)
	ctx := context.Background()

	if _, err := b.panel.Sell(ctx, "продавец завёл руками", "не из телеграма", Tariff{Days: 1}); err != nil {
		t.Fatalf("продажа: %v", err)
	}

	b.remindOnce(ctx)
	if len(tg.sent) != 0 {
		t.Fatalf("бот написал тому, кого не заводил: %q", tg.texts())
	}
}
