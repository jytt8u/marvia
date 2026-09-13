package panel

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/jytt8u/marvia/internal/users"
	"github.com/jytt8u/marvia/internal/vp1"
)

// ErrUnknownKind — попросили выпустить учётные данные неизвестного вида.
var ErrUnknownKind = errors.New("неизвестный вид учётных данных")

// trojanPasswordBytes — сколько случайности в пароле Trojan.
// 18 байт дают 24 символа base64 и 144 бита — перебирать нечего.
const trojanPasswordBytes = 18

// AddCredential выпускает подписчику ещё один набор доступа.
//
// Отдельный набор на устройство даёт три вещи, которых нет у схемы «один
// конфиг на человека»: видно, сколько устройств реально пользуется; любое
// отключается по отдельности; потерянный телефон не требует менять доступ
// на остальных. Квота при этом общая — все наборы связаны одним аккаунтом.
//
// Разница между видами принципиальная и её надо понимать.
//
// Для vp1 панель генерирует пару ключей, сохраняет только публичный, а
// приватный отдаёт один раз и забывает. Утечка базы панели не даёт доступа
// ни к одному клиенту.
//
// Для vless и trojan так нельзя: там секрет — это строка, которую клиент
// предъявляет целиком, и знать её обязаны обе стороны. Панель хранит её в
// открытом виде, потому что ноде эту строку надо отдать. Значит база панели
// становится настоящим секретом, и её утечка равносильна выдаче доступа
// всем клиентам, сидящим на чужих протоколах. Это и есть цена совместимости
// с существующими приложениями.
func (s *Store) AddCredential(ctx context.Context, userID int64, kind, label string) (Credential, string, error) {
	if kind == "" {
		kind = CredVP1
	}

	var exists int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM users WHERE id = ?`, userID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return Credential{}, "", ErrNotFound
	}
	if err != nil {
		return Credential{}, "", err
	}

	stored, shown, err := newSecret(kind)
	if err != nil {
		return Credential{}, "", err
	}

	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO credentials (user_id, kind, secret, label, created_at) VALUES (?, ?, ?, ?, ?)`,
		userID, kind, stored, label, format(now))
	if err != nil {
		return Credential{}, "", fmt.Errorf("создание набора доступа: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Credential{}, "", err
	}

	cred := Credential{ID: id, Kind: kind, Secret: stored, Label: label, CreatedAt: now}
	return cred, shown, nil
}

// newSecret готовит пару «что хранить» и «что показать один раз».
//
// Для vp1 это разные значения: хранится публичный ключ, показывается
// приватный. Для остальных видов значение одно и то же — в этом и разница.
func newSecret(kind string) (stored, shown string, err error) {
	switch kind {
	case CredVP1:
		pair, err := vp1.GenerateKeyPair()
		if err != nil {
			return "", "", fmt.Errorf("генерация ключа: %w", err)
		}
		return vp1.EncodeKey(pair.Public), vp1.EncodeKey(pair.Private), nil

	case CredVLESS:
		uuid, err := NewUUID()
		if err != nil {
			return "", "", err
		}
		return uuid, uuid, nil

	case CredTrojan:
		password, err := NewTrojanPassword()
		if err != nil {
			return "", "", err
		}
		return password, password, nil

	default:
		return "", "", fmt.Errorf("%w: %q", ErrUnknownKind, kind)
	}
}

// NewUUID выпускает UUID четвёртой версии.
func NewUUID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("генерация UUID: %w", err)
	}
	// Версия и вариант по RFC 4122: клиенты их проверяют, и без них
	// некоторые приложения откажутся принимать конфиг.
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return users.FormatUUID(raw), nil
}

// NewTrojanPassword выпускает пароль для Trojan.
func NewTrojanPassword() (string, error) {
	raw := make([]byte, trojanPasswordBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("генерация пароля: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// RotateCredential меняет секрет набора, не трогая сам набор.
//
// Зачем отдельная операция, когда есть «выдать новый» и «отозвать старый».
// Ключ утёк — переписка, потерянный телефон, чужой компьютер, — и продавцу
// нужно заменить именно его. Пара «добавить, удалить» даёт набору новый
// номер и новую пометку, а пометка у покупателя осмысленная: «телефон жены»,
// «рабочий ноутбук». После такой замены она относится уже к другой строке, и
// продавец, глядя в список из шести устройств, перестаёт понимать, где чьё.
//
// Здесь же остаются и номер, и пометка, и день выдачи: меняется только
// секрет. Расход и срок живут на аккаунте и не задеты в любом случае.
//
// Вид набора не меняется. Ключ vp1 и UUID vless — разные вещи для клиента:
// молча превратить одно в другое значило бы сломать приложение покупателя,
// не сказав ему ни слова. Нужен другой вид — это другой набор.
//
// Старый секрет пропадает без следа: ноды перечитывают список за секунды, и
// с этого момента он не подходит никуда.
func (s *Store) RotateCredential(ctx context.Context, id int64) (Credential, string, error) {
	var (
		cred      Credential
		createdAt string
		userID    int64
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, kind, label, created_at FROM credentials WHERE id = ?`, id).
		Scan(&cred.ID, &userID, &cred.Kind, &cred.Label, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Credential{}, "", ErrNotFound
	}
	if err != nil {
		return Credential{}, "", err
	}

	stored, shown, err := newSecret(cred.Kind)
	if err != nil {
		return Credential{}, "", err
	}

	res, err := s.db.ExecContext(ctx, `UPDATE credentials SET secret = ? WHERE id = ?`, stored, id)
	if err != nil {
		return Credential{}, "", fmt.Errorf("смена ключа: %w", err)
	}
	// Набор могли удалить между чтением и записью: это не «поменяли», а
	// «менять было нечего», и ответ должен быть таким же, как у пропажи.
	if n, _ := res.RowsAffected(); n == 0 {
		return Credential{}, "", ErrNotFound
	}

	cred.Secret = stored
	cred.CreatedAt = parse(createdAt)
	return cred, shown, nil
}

// UserByCredential находит хозяина набора доступа.
func (s *Store) UserByCredential(ctx context.Context, credID int64) (User, error) {
	var userID int64
	err := s.db.QueryRowContext(ctx, `SELECT user_id FROM credentials WHERE id = ?`, credID).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	return s.GetUser(ctx, userID)
}

// DeleteCredential отзывает один набор доступа.
//
// Отзыв мгновенный настолько, насколько быстро ноды перечитают список: до
// нескольких секунд. Расход аккаунта при этом сохраняется — статистика
// привязана к аккаунту, а не к набору.
func (s *Store) DeleteCredential(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM credentials WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("удаление набора доступа: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
