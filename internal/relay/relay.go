// Package relay перекладывает байты между двумя соединениями.
package relay

import (
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/veilproject/veil/internal/vp1"
)

// closeWriter — соединение, умеющее закрывать только исходящую половину.
type closeWriter interface{ CloseWrite() error }

// Bidirectional гоняет данные в обе стороны, пока обе не закончатся,
// и возвращает первую содержательную ошибку.
//
// Ключевая деталь — половинное закрытие. Когда одна сторона договорила,
// мы шлём второй CloseWrite вместо полного Close: иначе ответ, который она
// ещё не успела дописать, потеряется. На этом спотыкается большинство
// самодельных прокси, и проявляется это как «страница загрузилась наполовину».
func Bidirectional(a, b net.Conn) error {
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		fail error
	)

	record := func(err error) {
		if err == nil || isBenign(err) {
			return
		}
		mu.Lock()
		if fail == nil {
			fail = err
		}
		mu.Unlock()
	}

	pipe := func(dst, src net.Conn) {
		defer wg.Done()
		_, err := io.Copy(dst, src)
		record(err)
		if cw, ok := dst.(closeWriter); ok {
			_ = cw.CloseWrite()
		} else {
			_ = dst.Close()
		}
	}

	wg.Add(2)
	go pipe(a, b)
	go pipe(b, a)
	wg.Wait()

	_ = a.Close()
	_ = b.Close()

	mu.Lock()
	defer mu.Unlock()
	return fail
}

// isBenign отсеивает штатные окончания: закрытое соединение и EOF — это не
// сбой, а нормальный конец разговора.
func isBenign(err error) bool {
	return errors.Is(err, io.EOF) ||
		errors.Is(err, net.ErrClosed) ||
		errors.Is(err, io.ErrClosedPipe)
}

// Datagrams гоняет датаграммы между двумя соединениями с границами.
//
// От Bidirectional отличается двумя вещами, и обе — от природы UDP.
//
// Половинного закрытия нет: у датаграмм не бывает «я договорил», закрывать
// нечего и некому. Вместо этого есть срок простоя — единственный способ
// понять, что разговор кончился. Без него брошенные потоки копились бы до
// отключения человека от туннеля.
//
// Буфер во всю длину датаграммы: io.Copy взял бы свой, тридцать два килобайта,
// и датаграмма покрупнее не влезла бы. Это не усечение, а разрыв: дальше
// поехал бы её хвост, и все следующие датаграммы разъехались бы.
func Datagrams(a, b net.Conn, idle time.Duration) {
	var (
		wg   sync.WaitGroup
		once sync.Once
	)

	// Конец одной стороны — конец разговора: обратной дороги без прямой не
	// бывает. Закрываем обе, чтобы вторая горутина проснулась сразу, а не
	// досиживала срок простоя.
	stop := func() {
		once.Do(func() {
			_ = a.Close()
			_ = b.Close()
		})
	}

	pump := func(dst, src net.Conn) {
		defer wg.Done()
		defer stop()

		buf := make([]byte, vp1.MaxDatagram)
		for {
			_ = src.SetReadDeadline(time.Now().Add(idle))
			n, err := src.Read(buf)

			// Условие именно такое: датаграмма нулевой длины — это
			// датаграмма, а не «ничего не пришло». В UDP такие законны, ими
			// проверяют, что путь до собеседника жив. Отбросить её значит
			// потерять событие, и проявится это как молча отвалившееся
			// соединение, а не как потерянные байты.
			if err == nil || n > 0 {
				if _, err := dst.Write(buf[:n]); err != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}

	wg.Add(2)
	go pump(a, b)
	go pump(b, a)
	wg.Wait()
}
