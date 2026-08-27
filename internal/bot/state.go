package bot

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

// State — что бот помнит между запусками.
//
// Своей базы у бота нет: покупатели, сроки и расход живут в панели. Здесь
// только две вещи, которых панели знать неоткуда, — кому уже напомнили об
// окончании срока и какие платежи уже учтены.
type State struct {
	path string
	data stateFile
}

type stateFile struct {
	// Reminders: внешний ключ покупателя -> срок, о котором ему уже написали.
	Reminders map[string]string `json:"reminders"`

	// Charges: номер платежа -> когда учли.
	//
	// Телеграм повторяет уведомление об оплате, пока бот не ответит, а бот
	// может упасть ровно между выдачей доступа и ответом. Продление от повтора
	// защищает панель ключом идемпотентности, но первую продажу — нет: платежа
	// она не видит вовсе. Без этой отметки покупатель получал бы два месяца за
	// одни деньги.
	Charges map[string]time.Time `json:"charges"`
}

// chargeMemory — сколько помним платежи.
//
// Телеграм повторяет уведомление считанные часы; месяц с запасом закрывает
// любую задержку и не даёт файлу расти вечно.
const chargeMemory = 30 * 24 * time.Hour

// OpenState читает файл состояния, создавая его при необходимости.
func OpenState(path string) (*State, error) {
	s := &State{path: path, data: stateFile{
		Reminders: map[string]string{},
		Charges:   map[string]time.Time{},
	}}

	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("файл состояния: %w", err)
	}

	if err := json.Unmarshal(raw, &s.data); err != nil {
		// Битый файл не повод не работать: худшее, что случится, — кто-то
		// получит напоминание второй раз.
		log.Printf("состояние не разобралось (%v), начинаем с чистого", err)
		return s, nil
	}
	if s.data.Reminders == nil {
		s.data.Reminders = map[string]string{}
	}
	if s.data.Charges == nil {
		s.data.Charges = map[string]time.Time{}
	}

	s.forgetOldCharges()
	return s, nil
}

// remembered — писали ли уже этому покупателю про этот срок.
func (s *State) remembered(key, expiry string) bool { return s.data.Reminders[key] == expiry }

// remember отмечает, что напоминание отправлено.
func (s *State) remember(key, expiry string) {
	s.data.Reminders[key] = expiry
	s.save()
}

// forget убирает покупателей, которых больше нет: иначе файл растёт вечно.
func (s *State) forget(alive map[string]bool) {
	changed := false
	for key := range s.data.Reminders {
		if !alive[key] {
			delete(s.data.Reminders, key)
			changed = true
		}
	}
	if changed {
		s.save()
	}
}

// counted — учитывали ли уже этот платёж.
func (s *State) counted(charge string) bool {
	if charge == "" {
		return false
	}
	_, seen := s.data.Charges[charge]
	return seen
}

// count отмечает платёж учтённым.
func (s *State) count(charge string) {
	if charge == "" {
		return
	}
	s.data.Charges[charge] = time.Now().UTC()
	s.forgetOldCharges()
	s.save()
}

func (s *State) forgetOldCharges() {
	edge := time.Now().Add(-chargeMemory)
	for id, when := range s.data.Charges {
		if when.Before(edge) {
			delete(s.data.Charges, id)
		}
	}
}

func (s *State) save() {
	raw, err := json.Marshal(s.data)
	if err != nil {
		log.Printf("состояние не сохранилось: %v", err)
		return
	}

	// Пишем через временный файл: оборванная на середине запись оставила бы
	// обрезанный json, и бот при следующем запуске начал бы напоминать всем
	// заново, а учтённые платежи посчитал бы неучтёнными.
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		log.Printf("состояние не сохранилось: %v", err)
		return
	}
	if err := os.Rename(tmp, s.path); err != nil {
		log.Printf("состояние не сохранилось: %v", err)
	}
}

// StatePath — где по умолчанию лежит состояние.
func StatePath(dir string) string { return filepath.Join(dir, "veil-bot-state.json") }
