// Пример телеграм-бота продавца.
//
// Продавец покупает сервер, ставит панель и хочет продавать. Дальше у него
// один выбор: писать бота самому или уйти туда, где бот уже есть. Этот бот
// закрывает выбор — продавец заполняет файл настроек и запускает бинарник.
//
// Своей базы у бота нет и быть не должно. Кто что купил, когда кончается срок
// и сколько потрачено — знает панель, а бот ходит к ней по ключу с правом
// users. Потеря бота вместе с сервером не теряет ни одного покупателя.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/jytt8u/marvia/internal/envvar"
)

// Способы оплаты.
//
// Звёзды телеграма — единственный способ, для которого продавцу не нужен
// договор с платёжной системой: покупатель платит внутри телеграма, деньги
// приходят на счёт бота. Всё остальное в России упирается в юрлицо, поэтому
// второй способ — приём оплаты как умеет сам продавец, с подтверждением
// вручную.
const (
	PayStars  = "stars"
	PayManual = "manual"
)

// Config — всё, что продавец настраивает руками.
type Config struct {
	// Token — токен бота от @BotFather. Пусто — берём из MARVIA_BOT_TOKEN.
	Token string `json:"telegram_token"`

	// Panel — адрес панели, например https://panel.example.com.
	Panel string `json:"panel"`

	// Key — ключ доступа к панели с правом users. Пусто — из MARVIA_PANEL_KEY.
	//
	// Именно ключ, а не админский токен: бот стоит на сервере, доступном из
	// интернета, и при утечке ключ отзывается одним запросом, не выкидывая
	// продавца из собственной панели.
	Key string `json:"panel_key"`

	// Admins — telegram id тех, кому бот подчиняется. При оплате вручную
	// подтверждение приходит им.
	Admins []int64 `json:"admins"`

	// Payment — stars или manual.
	Payment string `json:"payment"`

	// ManualNote — что показать покупателю при оплате вручную: куда перевести
	// деньги и что написать в комментарии.
	ManualNote string `json:"manual_note"`

	// Support — куда писать, когда что-то не так. Обычно @имя продавца.
	Support string `json:"support"`

	// Downloads — откуда покупателю скачать приложения.
	Downloads Downloads `json:"downloads"`

	// Tariffs — что продаём. Порядок сохраняется: в каком записаны, в таком и
	// показываются.
	Tariffs []Tariff `json:"tariffs"`
}

// Downloads — ссылки на приложения.
type Downloads struct {
	Android string `json:"android"`
	Windows string `json:"windows"`
}

// Tariff — одна кнопка «купить».
type Tariff struct {
	ID    string `json:"id"`
	Title string `json:"title"`

	// Days — на сколько продлевает. Складывается с остатком: покупатель,
	// продливший заранее, ничего не теряет.
	Days int `json:"days"`

	// Price — цена. Для звёзд — в звёздах, для оплаты вручную — в тех
	// единицах, которые продавец написал в currency. Ноль означает выдачу
	// бесплатно; такой тариф достаётся только тем, кто ещё ничего не покупал.
	Price int64 `json:"price"`

	// Currency — как подписать цену при оплате вручную. По умолчанию ₽.
	Currency string `json:"currency,omitempty"`

	// TrafficGB — сколько гигабайт включено. Ноль — без ограничения.
	TrafficGB int64 `json:"traffic_gb"`

	// Devices — сколько адресов пускать одновременно. Ноль — без ограничения.
	Devices int `json:"devices"`
}

// LoadConfig читает и проверяет файл настроек.
//
// Проверяем строго и на месте: бот, запущенный с опечаткой в тарифе, узнает об
// этом от первого покупателя, а покупатель — от продавца, которому напишет.
func LoadConfig(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("файл настроек: %w", err)
	}

	var c Config
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("разбор настроек: %w", err)
	}

	if c.Token == "" {
		c.Token = envvar.Get("MARVIA_BOT_TOKEN")
	}
	if c.Key == "" {
		c.Key = envvar.Get("MARVIA_PANEL_KEY")
	}
	if c.Payment == "" {
		c.Payment = PayStars
	}

	return c, c.check()
}

func (c Config) check() error {
	if c.Token == "" {
		return errors.New("не задан токен бота: telegram_token в настройках или MARVIA_BOT_TOKEN")
	}
	if c.Key == "" {
		return errors.New("не задан ключ панели: panel_key в настройках или MARVIA_PANEL_KEY")
	}
	if !strings.HasPrefix(c.Panel, "http://") && !strings.HasPrefix(c.Panel, "https://") {
		return errors.New("panel: нужен адрес панели вида https://panel.example.com")
	}
	if c.Payment != PayStars && c.Payment != PayManual {
		return fmt.Errorf("payment: %q — бывает только stars или manual", c.Payment)
	}
	if c.Payment == PayManual && len(c.Admins) == 0 {
		return errors.New("при оплате вручную нужен хотя бы один admins: кому-то надо подтверждать платежи")
	}
	if len(c.Tariffs) == 0 {
		return errors.New("не задано ни одного тарифа: продавать нечего")
	}

	seen := make(map[string]bool, len(c.Tariffs))
	for i, t := range c.Tariffs {
		switch {
		case t.ID == "":
			return fmt.Errorf("тариф %d: пустой id", i+1)
		case seen[t.ID]:
			return fmt.Errorf("тариф %q встречается дважды", t.ID)
		case strings.ContainsAny(t.ID, ": "):
			// id уезжает в callback_data кнопки, а там двоеточие разделяет поля.
			return fmt.Errorf("тариф %q: в id нельзя двоеточие и пробел", t.ID)
		case t.Title == "":
			return fmt.Errorf("тариф %q: пустое название", t.ID)
		case t.Days <= 0:
			return fmt.Errorf("тариф %q: срок в днях должен быть больше нуля", t.ID)
		case t.Price < 0:
			return fmt.Errorf("тариф %q: цена не может быть отрицательной", t.ID)
		}
		seen[t.ID] = true
	}

	return nil
}

// tariff находит тариф по id.
func (c Config) tariff(id string) (Tariff, bool) {
	for _, t := range c.Tariffs {
		if t.ID == id {
			return t, true
		}
	}
	return Tariff{}, false
}

// isAdmin — подчиняется ли бот этому человеку.
func (c Config) isAdmin(id int64) bool {
	for _, a := range c.Admins {
		if a == id {
			return true
		}
	}
	return false
}

// currency — как подписать цену при оплате вручную.
func (t Tariff) currency() string {
	if t.Currency == "" {
		return "₽"
	}
	return t.Currency
}

// trafficBytes — лимит трафика в байтах, как его понимает панель.
func (t Tariff) trafficBytes() int64 {
	if t.TrafficGB <= 0 {
		return 0
	}
	return t.TrafficGB * 1024 * 1024 * 1024
}
