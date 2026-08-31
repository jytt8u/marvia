// Команда gen собирает список российских подсетей для байпаса.
//
// Запускается руками, когда список пора обновить:
//
//	go run ./internal/routes/gen internal/routes/ru.txt.gz
//
// Источник — реестр RIPE. Он отдаёт диапазоны началом и длиной, причём длина
// не обязана быть степенью двойки: 1.2.3.0 на 768 адресов — это /24 и /23
// рядом. Поэтому диапазоны режутся на подсети, а соседние потом склеиваются
// обратно, чтобы список не разрастался.
package main

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"math/bits"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
)

const source = "https://ftp.ripe.net/pub/stats/ripencc/delegated-ripencc-latest"

func main() {
	out := "internal/routes/ru.txt.gz"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}

	local := ""
	if len(os.Args) > 2 {
		local = os.Args[2]
	}

	ranges, err := fetch(local)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ошибка:", err)
		os.Exit(1)
	}

	prefixes := merge(ranges)

	if err := write(out, prefixes); err != nil {
		fmt.Fprintln(os.Stderr, "ошибка:", err)
		os.Exit(1)
	}

	fmt.Printf("диапазонов из реестра: %d, подсетей после склейки: %d, файл: %s\n",
		len(ranges), len(prefixes), out)
}

// ipRange — диапазон адресов, как его отдаёт реестр.
type ipRange struct{ from, to uint32 }

// fetch читает реестр. Путь к уже скачанному файлу можно передать вторым
// аргументом: у самого реестра бывают перебои, а список нужен именно сейчас.
func fetch(local string) ([]ipRange, error) {
	var body io.ReadCloser
	if local != "" {
		file, err := os.Open(local)
		if err != nil {
			return nil, err
		}
		body = file
	} else {
		resp, err := http.Get(source) //nolint:gosec,noctx // разовая утилита, адрес постоянный
		if err != nil {
			return nil, fmt.Errorf("реестр RIPE: %w", err)
		}
		body = resp.Body
	}
	defer body.Close()

	var out []ipRange

	scan := bufio.NewScanner(body)
	scan.Buffer(make([]byte, 1<<20), 1<<20)
	for scan.Scan() {
		// ripencc|RU|ipv4|2.60.0.0|262144|20120401|allocated
		parts := strings.Split(scan.Text(), "|")
		if len(parts) < 5 || parts[1] != "RU" || parts[2] != "ipv4" {
			continue
		}

		start, ok := parseIP(parts[3])
		if !ok {
			continue
		}
		count, err := strconv.ParseUint(parts[4], 10, 32)
		if err != nil || count == 0 {
			continue
		}

		out = append(out, ipRange{from: start, to: start + uint32(count) - 1})
	}
	return out, scan.Err()
}

func parseIP(s string) (uint32, bool) {
	var v uint32
	for i, part := range strings.Split(s, ".") {
		if i > 3 {
			return 0, false
		}
		n, err := strconv.ParseUint(part, 10, 8)
		if err != nil {
			return 0, false
		}
		v = v<<8 | uint32(n)
	}
	return v, true
}

// merge склеивает соседние диапазоны и режет их на подсети.
//
// Склейка до нарезки, а не после: соседние выделения одного провайдера часто
// стыкуются встык, и вместе они дают одну короткую подсеть вместо десятка
// длинных.
func merge(ranges []ipRange) []string {
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].from < ranges[j].from })

	var joined []ipRange
	for _, r := range ranges {
		if n := len(joined); n > 0 && r.from <= joined[n-1].to+1 {
			if r.to > joined[n-1].to {
				joined[n-1].to = r.to
			}
			continue
		}
		joined = append(joined, r)
	}

	var out []string
	for _, r := range joined {
		out = append(out, split(r.from, r.to)...)
	}
	return out
}

// split режет диапазон на подсети наибольшего возможного размера.
func split(from, to uint32) []string {
	var out []string
	for from <= to {
		// Размер ограничен и выравниванием начала, и остатком до конца.
		size := uint32(1) << bits.TrailingZeros32(from)
		if from == 0 {
			size = 1 << 31
		}
		for size-1 > to-from {
			size >>= 1
		}

		bitsLeft := 32 - bits.TrailingZeros32(size)
		out = append(out, fmt.Sprintf("%d.%d.%d.%d/%d",
			from>>24, from>>16&0xff, from>>8&0xff, from&0xff, bitsLeft))

		if from+size-1 == to || from+size < from {
			break
		}
		from += size
	}
	return out
}

func write(path string, prefixes []string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	zip, err := gzip.NewWriterLevel(file, gzip.BestCompression)
	if err != nil {
		return err
	}
	defer zip.Close()

	for _, p := range prefixes {
		if _, err := fmt.Fprintln(zip, p); err != nil {
			return err
		}
	}
	return nil
}
