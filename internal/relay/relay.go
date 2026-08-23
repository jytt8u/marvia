// Package relay перекладывает байты между двумя соединениями.
package relay

import (
	"errors"
	"io"
	"net"
	"sync"
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
