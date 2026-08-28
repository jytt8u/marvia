package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

// State — что бот помнит между запусками.
//
// Своей базы у бота нет: покупатели, сроки и расход живут в панели. Здесь
// только одно, чего панели знать неоткуда, — кому уже напомнили об окончании
// срока.
//
// Про платежи бот тоже ничего не помнит: повтор уведомления гасит панель по
// заголовку Idempotency-Key, и на продлении, и на первой продаже.
type State struct {
	path string
	data stateFile
}

type stateFile struct {
	// Reminders: внешний ключ покупателя -> срок, о котором ему уже написали.
	Reminders map[string]string `json:"reminders"`
}

// OpenState читает файл состояния, создавая его при необходимости.
func OpenState(path string) (*State, error) {
	s := &State{path: path, data: stateFile{Reminders: map[string]string{}}}

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
func StatePath(dir string) string { return filepath.Join(dir, "marvia-bot-state.json") }
