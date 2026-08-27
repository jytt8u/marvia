package bot

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log"
	"strconv"
	"strings"
	"time"
)

// Bot — сам бот.
type Bot struct {
	cfg   Config
	tg    *telegram
	panel *Panel
	state *State
}

// New собирает бота из настроек.
func New(cfg Config, state *State) *Bot {
	return &Bot{
		cfg:   cfg,
		tg:    newTelegram(cfg.Token),
		panel: NewPanel(cfg.Panel, cfg.Key),
		state: state,
	}
}

// Run крутит опрос телеграма, пока не отменят контекст.
func (b *Bot) Run(ctx context.Context) error {
	go b.remind(ctx)

	var offset int64
	for {
		if ctx.Err() != nil {
			return nil
		}

		updates, err := b.tg.updates(ctx, offset)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			// Обрыв связи с телеграмом — обычное дело на сервере, который
			// сам стоит за блокировками. Молчим и пробуем снова: падать
			// нельзя, иначе бот умрёт от первого сетевого сбоя.
			log.Printf("опрос телеграма: %v", err)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(5 * time.Second):
			}
			continue
		}

		for _, u := range updates {
			offset = u.UpdateID + 1
			b.handle(ctx, u)
		}
	}
}

func (b *Bot) handle(ctx context.Context, u Update) {
	// Каждое событие обрабатываем отдельно и с запасом времени: покупатель
	// не должен ждать дольше, чем ему кажется разумным, а бот не должен
	// вставать из-за одного застрявшего запроса.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	switch {
	case u.PreCheckoutQuery != nil:
		b.preCheckout(ctx, u.PreCheckoutQuery)
	case u.CallbackQuery != nil:
		b.callback(ctx, u.CallbackQuery)
	case u.Message != nil:
		b.message(ctx, u.Message)
	}
}

func (b *Bot) message(ctx context.Context, m *Message) {
	if m.From == nil || m.From.IsBot {
		return
	}

	if pay := m.SuccessfulPayment; pay != nil {
		b.paid(ctx, m.From, pay)
		return
	}

	// Команду берём первым словом: телеграм дописывает к ней имя бота в
	// групповых чатах, а в личке присылает как есть.
	command, _, _ := strings.Cut(strings.Fields(m.Text + " ")[0], "@")

	switch strings.ToLower(command) {
	case "/id":
		// Продавцу нужен свой telegram id, чтобы вписать его в admins. Без
		// этого первая же настройка упирается в поиск стороннего бота.
		b.say(ctx, m.Chat.ID, fmt.Sprintf("Твой telegram id: <code>%d</code>", m.From.ID))
	default:
		b.start(ctx, m.Chat.ID, m.From)
	}
}

// start — первый экран.
func (b *Bot) start(ctx context.Context, chat int64, from *TGUser) {
	name := html.EscapeString(strings.TrimSpace(from.FirstName))
	greeting := "Привет"
	if name != "" {
		greeting += ", " + name
	}

	text := greeting + "!\n\n" +
		"Здесь покупается доступ в интернет без ограничений. " +
		"После оплаты бот пришлёт ссылку — её надо нажать, и приложение настроится само."

	b.say(ctx, chat, text, b.mainButtons()...)
}

func (b *Bot) mainButtons() [][]Button {
	rows := [][]Button{
		{{Text: "Купить доступ", Data: "buy"}},
		{{Text: "Моя подписка", Data: "status"}},
		{{Text: "Приложения", Data: "apps"}},
	}
	if b.cfg.Support != "" {
		rows = append(rows, []Button{{Text: "Написать в поддержку", URL: supportURL(b.cfg.Support)}})
	}
	return rows
}

func (b *Bot) callback(ctx context.Context, q *CallbackQuery) {
	if q.Message == nil {
		return
	}
	chat := q.Message.Chat.ID

	// «Часики» на кнопке гасим сразу: телеграм крутит их пять секунд, и
	// покупатель, не увидевший ответа, жмёт ещё раз.
	defer func() { _ = b.tg.answer(context.WithoutCancel(ctx), q.ID, "") }()

	action, arg, _ := strings.Cut(q.Data, ":")
	switch action {
	case "buy":
		if arg == "" {
			b.showTariffs(ctx, chat)
			return
		}
		b.wantsToBuy(ctx, chat, &q.From, arg)

	case "paid":
		b.claimsPaid(ctx, chat, &q.From, arg)

	case "grant":
		b.grant(ctx, chat, &q.From, arg)

	case "status":
		b.status(ctx, chat, &q.From)

	case "apps":
		b.apps(ctx, chat)

	case "newkey":
		b.newKey(ctx, chat, &q.From)
	}
}

func (b *Bot) showTariffs(ctx context.Context, chat int64) {
	rows := make([][]Button, 0, len(b.cfg.Tariffs))
	for _, t := range b.cfg.Tariffs {
		rows = append(rows, []Button{{Text: b.tariffButton(t), Data: "buy:" + t.ID}})
	}
	b.say(ctx, chat, "Выбери срок:", rows...)
}

func (b *Bot) tariffButton(t Tariff) string {
	if t.Price == 0 {
		return t.Title + " — бесплатно"
	}
	if b.cfg.Payment == PayStars {
		return fmt.Sprintf("%s — %d ⭐", t.Title, t.Price)
	}
	return fmt.Sprintf("%s — %d %s", t.Title, t.Price, t.currency())
}

// wantsToBuy — покупатель выбрал тариф.
func (b *Bot) wantsToBuy(ctx context.Context, chat int64, from *TGUser, id string) {
	t, found := b.cfg.tariff(id)
	if !found {
		b.say(ctx, chat, "Такого тарифа больше нет. Нажми «Купить доступ» ещё раз.")
		return
	}

	// Бесплатный тариф — только тем, кого панель ещё не знает. Иначе он
	// становится бесконечным: удалил переписку, начал заново.
	if t.Price == 0 {
		if _, err := b.panel.ByExternalID(ctx, external(from.ID)); !errors.Is(err, ErrNoUser) {
			b.say(ctx, chat, "Бесплатный доступ выдаётся один раз, и он у тебя уже был. Выбери срок из платных.")
			return
		}
		b.deliver(ctx, chat, from, t, "")
		return
	}

	if b.cfg.Payment == PayManual {
		note := b.cfg.ManualNote
		if note == "" {
			note = "Продавец подтвердит оплату вручную."
		}
		text := fmt.Sprintf("<b>%s</b> — %d %s\n\n%s\n\nПосле оплаты нажми кнопку ниже.",
			html.EscapeString(t.Title), t.Price, t.currency(), html.EscapeString(note))
		b.say(ctx, chat, text, []Button{{Text: "Я оплатил", Data: "paid:" + t.ID}})
		return
	}

	desc := fmt.Sprintf("Доступ на %d дней", t.Days)
	if err := b.tg.invoice(ctx, chat, t.Title, desc, "t:"+t.ID, t.Price); err != nil {
		log.Printf("счёт для %d: %v", from.ID, err)
		b.say(ctx, chat, "Не получилось выставить счёт. Попробуй ещё раз через минуту.")
	}
}

// preCheckout — последняя проверка перед списанием.
func (b *Bot) preCheckout(ctx context.Context, q *PreCheckoutQuery) {
	id := strings.TrimPrefix(q.Payload, "t:")
	if _, found := b.cfg.tariff(id); !found {
		_ = b.tg.approve(ctx, q.ID, false, "Этот тариф больше не продаётся")
		return
	}
	if err := b.tg.approve(ctx, q.ID, true, ""); err != nil {
		log.Printf("подтверждение счёта: %v", err)
	}
}

// paid — телеграм списал деньги.
func (b *Bot) paid(ctx context.Context, from *TGUser, pay *SuccessfulPayment) {
	id := strings.TrimPrefix(pay.Payload, "t:")
	t, found := b.cfg.tariff(id)
	if !found {
		// Тариф успели убрать из настроек между счётом и оплатой. Деньги
		// взяты, значит доступ должен быть выдан: сообщаем продавцу.
		b.tellAdmins(ctx, fmt.Sprintf("Оплачен неизвестный тариф %q, покупатель %d. Выдай доступ вручную.", id, from.ID))
		b.say(ctx, from.ID, "Оплата прошла, но с тарифом вышла путаница. Продавец уже знает и сейчас всё выдаст.")
		return
	}

	b.deliver(ctx, from.ID, from, t, pay.ChargeID)
}

// claimsPaid — покупатель говорит, что перевёл деньги.
func (b *Bot) claimsPaid(ctx context.Context, chat int64, from *TGUser, id string) {
	t, found := b.cfg.tariff(id)
	if !found {
		b.say(ctx, chat, "Такого тарифа больше нет. Нажми «Купить доступ» ещё раз.")
		return
	}

	b.say(ctx, chat, "Передал продавцу. Как только он подтвердит оплату, бот пришлёт доступ.")

	who := html.EscapeString(from.FirstName)
	if from.Username != "" {
		who += " (@" + html.EscapeString(from.Username) + ")"
	}
	text := fmt.Sprintf("Покупатель %s, id <code>%d</code>, говорит что оплатил <b>%s</b> — %d %s.",
		who, from.ID, html.EscapeString(t.Title), t.Price, t.currency())

	// Кнопка несёт в себе и покупателя, и тариф: своей памяти у бота нет, и
	// перезапуск между «я оплатил» и подтверждением ничего не теряет.
	b.tellAdmins(ctx, text, []Button{{
		Text: "Выдать доступ",
		Data: fmt.Sprintf("grant:%d.%s", from.ID, t.ID),
	}})
}

// grant — продавец подтвердил оплату.
func (b *Bot) grant(ctx context.Context, chat int64, from *TGUser, arg string) {
	if !b.cfg.isAdmin(from.ID) {
		b.say(ctx, chat, "Эта кнопка не для тебя.")
		return
	}

	rawID, tariffID, ok := strings.Cut(arg, ".")
	if !ok {
		return
	}
	buyer, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		return
	}
	t, found := b.cfg.tariff(tariffID)
	if !found {
		b.say(ctx, chat, "Тариф "+html.EscapeString(tariffID)+" пропал из настроек.")
		return
	}

	// Ключ идемпотентности из покупателя и тарифа: два нажатия на одну и ту
	// же кнопку не дадут двух сроков.
	key := fmt.Sprintf("manual:%d:%s", buyer, t.ID)
	b.deliver(ctx, buyer, &TGUser{ID: buyer}, t, key)
	b.say(ctx, chat, "Выдал: "+html.EscapeString(t.Title)+" покупателю "+rawID+".")
}

// deliver заводит или продлевает подписку и отправляет всё покупателю.
//
// key — номер платежа. Один и тот же платёж выдаёт доступ ровно один раз:
// телеграм повторяет уведомление, пока бот не ответил, а бот может упасть
// ровно между выдачей и ответом. Продление от повтора защищает панель ключом
// идемпотентности, но первую продажу — нет: про платежи она не знает вовсе,
// поэтому повтор той самой оплаты, которая завела покупателя, добавил бы ему
// второй срок бесплатно.
func (b *Bot) deliver(ctx context.Context, chat int64, from *TGUser, t Tariff, key string) {
	ext := external(from.ID)

	if b.state.counted(key) {
		b.alreadyCounted(ctx, chat, ext)
		return
	}

	user, err := b.panel.ByExternalID(ctx, ext)
	switch {
	case errors.Is(err, ErrNoUser):
		b.sell(ctx, chat, from, t, key)
		return
	case err != nil:
		log.Printf("поиск покупателя %s: %v", ext, err)
		b.failed(ctx, chat)
		return
	}

	fresh, err := b.panel.Extend(ctx, user.ID, t, key)
	if err != nil {
		log.Printf("продление %s: %v", ext, err)
		b.failed(ctx, chat)
		return
	}
	b.state.count(key)

	b.say(ctx, chat, "Подписка продлена до <b>"+until(fresh.ExpiresAt)+"</b>.\n\nНичего менять не нужно — доступ уже работает.",
		[]Button{{Text: "Моя подписка", Data: "status"}})
}

// alreadyCounted отвечает на повтор уже учтённого платежа.
//
// Новых ссылок здесь не выдаём: покупатель их уже получил, а лишний набор
// доступа на каждое повторное уведомление — мусор в панели.
func (b *Bot) alreadyCounted(ctx context.Context, chat int64, ext string) {
	user, err := b.panel.ByExternalID(ctx, ext)
	if err != nil {
		b.say(ctx, chat, "Эта оплата уже учтена.")
		return
	}
	b.say(ctx, chat, "Эта оплата уже учтена — доступ работает до <b>"+until(user.ExpiresAt)+"</b>.",
		[]Button{{Text: "Моя подписка", Data: "status"}})
}

func (b *Bot) sell(ctx context.Context, chat int64, from *TGUser, t Tariff, key string) {
	label := strings.TrimSpace(from.FirstName)
	if from.Username != "" {
		label = "@" + from.Username
	}
	if label == "" {
		label = "покупатель " + strconv.FormatInt(from.ID, 10)
	}

	sale, err := b.panel.Sell(ctx, external(from.ID), label, t)
	if err != nil {
		log.Printf("продажа %d: %v", from.ID, err)
		b.failed(ctx, chat)
		return
	}
	b.state.count(key)

	b.sendAccess(ctx, chat, sale.Links, sale.User.ExpiresAt)
}

// sendAccess отправляет доступ так, чтобы человек справился без объяснений.
//
// Ссылка отдельным сообщением и одной строкой: её будут копировать, и всё
// лишнее рядом мешает. Наша ссылка первой — по ней приложение настраивается
// само; ссылка подписки ниже, для тех, у кого уже стоит чужой клиент.
func (b *Bot) sendAccess(ctx context.Context, chat int64, links Links, expires *time.Time) {
	text := "Готово. Доступ работает до <b>" + until(expires) + "</b>.\n\n" +
		"Ниже ссылка. Нажми на неё — приложение подхватит ключ само."
	b.say(ctx, chat, text)

	if links.Account != "" {
		b.say(ctx, chat, links.Account)
	}

	rows := b.appButtons()
	tail := "Приложение ставится один раз."
	if links.Subscription != "" {
		tail = "Если у тебя уже стоит v2rayNG, Hiddify или NekoBox — добавь туда подписку:\n\n<code>" +
			html.EscapeString(links.Subscription) + "</code>"
	}
	b.say(ctx, chat, tail, rows...)
}

func (b *Bot) apps(ctx context.Context, chat int64) {
	rows := b.appButtons()
	if len(rows) == 0 {
		b.say(ctx, chat, "Ссылки на приложения продавец ещё не настроил.")
		return
	}
	b.say(ctx, chat, "Скачай приложение под своё устройство:", rows...)
}

func (b *Bot) appButtons() [][]Button {
	var rows [][]Button
	if b.cfg.Downloads.Android != "" {
		rows = append(rows, []Button{{Text: "Android", URL: b.cfg.Downloads.Android}})
	}
	if b.cfg.Downloads.Windows != "" {
		rows = append(rows, []Button{{Text: "Windows", URL: b.cfg.Downloads.Windows}})
	}
	return rows
}

func (b *Bot) status(ctx context.Context, chat int64, from *TGUser) {
	user, err := b.panel.ByExternalID(ctx, external(from.ID))
	if errors.Is(err, ErrNoUser) {
		b.say(ctx, chat, "Подписки пока нет.", []Button{{Text: "Купить доступ", Data: "buy"}})
		return
	}
	if err != nil {
		log.Printf("состояние %d: %v", from.ID, err)
		b.failed(ctx, chat)
		return
	}

	var text strings.Builder
	switch {
	case !user.Enabled:
		text.WriteString("Доступ отключён. Напиши продавцу.")
	case user.ExpiresAt == nil:
		text.WriteString("Доступ работает <b>без ограничения по сроку</b>.")
	case user.ExpiresAt.Before(time.Now()):
		text.WriteString("Срок вышел " + until(user.ExpiresAt) + ". Продли — доступ включится сразу.")
	default:
		text.WriteString("Доступ работает до <b>" + until(user.ExpiresAt) + "</b>.")
	}

	if user.TrafficLimit > 0 {
		left := user.TrafficLimit - user.Used
		if left < 0 {
			left = 0
		}
		fmt.Fprintf(&text, "\nОсталось %s из %s.", size(left), size(user.TrafficLimit))
	} else {
		fmt.Fprintf(&text, "\nИзрасходовано %s, ограничения нет.", size(user.Used))
	}

	b.say(ctx, chat, text.String(),
		[]Button{{Text: "Продлить", Data: "buy"}},
		[]Button{{Text: "Новая ссылка доступа", Data: "newkey"}},
	)
}

// newKey выдаёт ссылку заново.
//
// Старую взять неоткуда: приватной части панель не хранит вовсе. Поэтому
// «потерял ссылку» и «поставил на второй телефон» решаются одинаково — новым
// набором доступа. Срок и квота у них общие, лишнего трафика это не даёт.
func (b *Bot) newKey(ctx context.Context, chat int64, from *TGUser) {
	user, err := b.panel.ByExternalID(ctx, external(from.ID))
	if errors.Is(err, ErrNoUser) {
		b.say(ctx, chat, "Подписки пока нет.", []Button{{Text: "Купить доступ", Data: "buy"}})
		return
	}
	if err != nil {
		log.Printf("ссылка для %d: %v", from.ID, err)
		b.failed(ctx, chat)
		return
	}

	link, err := b.panel.NewDevice(ctx, user.ID)
	if err != nil {
		log.Printf("новый набор для %d: %v", from.ID, err)
		b.failed(ctx, chat)
		return
	}

	b.say(ctx, chat, "Новая ссылка. Нажми на неё — приложение подхватит ключ:")
	b.say(ctx, chat, link)
}

// failed — единственная фраза на все внутренние поломки.
//
// Покупателю нечего делать с текстом ошибки, а продавцу он и так уходит в
// журнал. Обещать «сейчас починится» тоже не надо: если панель лежит, врать
// будет бот, а отвечать — продавец.
func (b *Bot) failed(ctx context.Context, chat int64) {
	text := "Что-то пошло не так на нашей стороне. Попробуй через минуту"
	if b.cfg.Support != "" {
		text += ", а если не выйдет — напиши " + html.EscapeString(b.cfg.Support)
	}
	b.say(ctx, chat, text+".")
}

func (b *Bot) say(ctx context.Context, chat int64, text string, rows ...[]Button) {
	if err := b.tg.send(ctx, chat, text, rows...); err != nil {
		log.Printf("сообщение в %d: %v", chat, err)
	}
}

func (b *Bot) tellAdmins(ctx context.Context, text string, rows ...[]Button) {
	for _, id := range b.cfg.Admins {
		b.say(ctx, id, text, rows...)
	}
}

// external — как покупатель называется в панели.
func external(telegramID int64) string {
	return "tg:" + strconv.FormatInt(telegramID, 10)
}

// supportURL превращает @имя в ссылку.
func supportURL(support string) string {
	if strings.HasPrefix(support, "http") {
		return support
	}
	return "https://t.me/" + strings.TrimPrefix(support, "@")
}

var months = [...]string{
	"января", "февраля", "марта", "апреля", "мая", "июня",
	"июля", "августа", "сентября", "октября", "ноября", "декабря",
}

// until — дата по-русски. Покупателю нужна дата, а не отметка времени.
func until(t *time.Time) string {
	if t == nil {
		return "без ограничения"
	}
	local := t.Local()
	return fmt.Sprintf("%d %s %d", local.Day(), months[int(local.Month())-1], local.Year())
}

// size — объём словами, которые люди читают на своих тарифах.
func size(bytes int64) string {
	const gb = 1024 * 1024 * 1024
	switch {
	case bytes >= gb:
		return fmt.Sprintf("%.1f ГБ", float64(bytes)/gb)
	case bytes >= 1024*1024:
		return fmt.Sprintf("%d МБ", bytes/(1024*1024))
	default:
		return fmt.Sprintf("%d КБ", bytes/1024)
	}
}
