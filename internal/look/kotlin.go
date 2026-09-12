package look

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Приложение на Android не умеет читать look.js: там нет браузера, а тащить
// движок JavaScript ради таблицы из тридцати строк — безумие. Поэтому таблица
// переписывается в Kotlin генератором — из того же файла, который вшивается
// в панель и окно. Правка цвета в look.js и `go generate ./internal/look`
// красят все три клиента; тест следит, что Kotlin не отстал.

//go:generate go run ./gen

// Preset — одна тема из таблицы: фон, текст, акцент.
type Preset struct {
	Key         string
	BG, FG, Acc string
}

// Density — одна плотность: отступ карточки и зазор между ними.
type Density struct {
	Key      string
	Pad, Gap int
}

// Radius — одно скругление: ключ и величина в px/dp.
type Radius struct {
	Key string
	R   int
}

// Glow — сила свечения: ключ и доля от единицы.
type Glow struct {
	Key string
	A   float64
}

// Look — готовый вид: пресет и все ручки разом.
type Look struct {
	Preset, Kind, Dir          string
	Depth                      float64
	Radius, Density, Btn, Glow string
	Card                       string
}

// PresetList отдаёт пресеты в порядке таблицы. Порядок — часть договора: так
// они стоят на экране выбора во всех клиентах, и человек, привыкший к месту
// «Изумруда» в панели, найдёт его там же в телефоне.
func PresetList() []Preset {
	var out []Preset
	for _, m := range presetRow.FindAllStringSubmatch(js, -1) {
		out = append(out, Preset{Key: m[1], BG: m[2], FG: m[3], Acc: m[4]})
	}
	return out
}

// Densities отдаёт плотности в порядке таблицы.
func Densities() []Density {
	var out []Density
	for _, m := range densityRow.FindAllStringSubmatch(js, -1) {
		pad, _ := strconv.Atoi(m[2])
		gap, _ := strconv.Atoi(m[3])
		out = append(out, Density{Key: m[1], Pad: pad, Gap: gap})
	}
	return out
}

// Radii отдаёт скругления в порядке таблицы.
func Radii() []Radius {
	var out []Radius
	for _, m := range keyNumRow.FindAllStringSubmatch(line("RADII"), -1) {
		r, _ := strconv.Atoi(m[2])
		out = append(out, Radius{Key: m[1], R: r})
	}
	return out
}

// Glows отдаёт силы свечения в порядке таблицы.
func Glows() []Glow {
	var out []Glow
	for _, m := range keyNumRow.FindAllStringSubmatch(line("GLOWS"), -1) {
		a, _ := strconv.ParseFloat(m[2], 64)
		out = append(out, Glow{Key: m[1], A: a})
	}
	return out
}

// LookList отдаёт готовые виды в порядке таблицы.
func LookList() []Look {
	var out []Look
	for _, m := range lookRow.FindAllStringSubmatch(js, -1) {
		depth, _ := strconv.ParseFloat(m[4], 64)
		out = append(out, Look{Preset: m[1], Kind: m[2], Dir: m[3], Depth: depth,
			Radius: m[5], Density: m[6], Btn: m[7], Glow: m[8], Card: m[9]})
	}
	return out
}

// Words отдаёт список слов из строки вида `const NAME = ["a", "b"]`.
func Words(name string) []string {
	var out []string
	for _, m := range regexp.MustCompile(`"([a-z]+)"`).FindAllStringSubmatch(line(name), -1) {
		out = append(out, m[1])
	}
	return out
}

// Dirs отдаёт стороны света с углом CSS в порядке таблицы.
func Dirs() []Dir {
	var out []Dir
	block := regexp.MustCompile(`const DIRS = \{[^}]*\}`).FindString(js)
	for _, m := range regexp.MustCompile(`([a-z]+): \[(\d+), "([a-z ]+)"\]`).FindAllStringSubmatch(block, -1) {
		deg, _ := strconv.Atoi(m[2])
		out = append(out, Dir{Key: m[1], Deg: deg, Pos: m[3]})
	}
	return out
}

// Dir — откуда светит: ключ, угол CSS-градиента и положение пятна.
type Dir struct {
	Key string
	Deg int
	Pos string
}

// line — строка объявления const NAME = … целиком.
func line(name string) string {
	return regexp.MustCompile(`const ` + name + ` = [^\n]*`).FindString(js)
}

var (
	presetRow  = regexp.MustCompile(`(?m)^    ([a-z]+): +\{ bg: "(#[0-9a-f]{6})", fg: "(#[0-9a-f]{6})", acc: "(#[0-9a-f]{6})" \}`)
	densityRow = regexp.MustCompile(`(?m)^    ([a-z]+): +\{ pad: (\d+), gap: (\d+) \}`)
	keyNumRow  = regexp.MustCompile(`([a-z]+): ([0-9.]+)`)
	lookRow    = regexp.MustCompile(`\{ preset: "([a-z]+)", +kind: "([a-z]+)", +dir: "([a-z]+)", +depth: ([0-9.]+), radius: "([a-z]+)", +density: "([a-z]+)", +btn: "([a-z]+)", +glow: "([a-z]+)", +card: "([a-z]+)" \}`)
)

// Kotlin собирает файл таблицы для приложения на Android.
//
// Только данные: арифметика темы — mix, контраст, подбор приглушённого,
// градиент фона — написана на Kotlin руками в Look.kt, потому что
// генерировать код из кода значило бы породить второй, нечитаемый язык.
// Числа же переписывать руками нельзя: их сотни, и опечатка в одном не видна
// ни на каком экране, кроме экрана того, кто выбрал именно этот пресет.
func Kotlin() string {
	var b strings.Builder
	b.WriteString("package io.marvia.android\n\n")
	b.WriteString("// Файл собран генератором из internal/look/look.js — руками не править.\n")
	b.WriteString("// Обновить: go generate ./internal/look. Тест в том же пакете следит,\n")
	b.WriteString("// что этот файл не отстал от таблицы.\n\n")
	b.WriteString("/** Таблица тем, общая с панелью и окном на компьютере. */\n")
	b.WriteString("object LookTable {\n")

	b.WriteString("    /** Пресеты в порядке экрана выбора: ключ, фон, текст, акцент. */\n")
	b.WriteString("    val presets: List<Preset> = listOf(\n")
	for _, p := range PresetList() {
		fmt.Fprintf(&b, "        Preset(%q, 0x%s, 0x%s, 0x%s),\n", p.Key, argb(p.BG), argb(p.FG), argb(p.Acc))
	}
	b.WriteString("    )\n\n")

	b.WriteString("    /** Плотности: ключ, отступ карточки, зазор — в dp. */\n")
	b.WriteString("    val densities: List<Density> = listOf(\n")
	for _, d := range Densities() {
		fmt.Fprintf(&b, "        Density(%q, %d, %d),\n", d.Key, d.Pad, d.Gap)
	}
	b.WriteString("    )\n\n")

	b.WriteString("    /** Скругления: ключ и радиус в dp. */\n")
	b.WriteString("    val radii: List<Radius> = listOf(\n")
	for _, r := range Radii() {
		fmt.Fprintf(&b, "        Radius(%q, %d),\n", r.Key, r.R)
	}
	b.WriteString("    )\n\n")

	b.WriteString("    /** Свечение: ключ и сила от 0 до 1. */\n")
	b.WriteString("    val glows: List<Glow> = listOf(\n")
	for _, g := range Glows() {
		fmt.Fprintf(&b, "        Glow(%q, %s),\n", g.Key, kotlinFloat(g.A))
	}
	b.WriteString("    )\n\n")

	b.WriteString("    /** Откуда светит: ключ и угол CSS-градиента; «c» — из центра. */\n")
	b.WriteString("    val dirs: List<Dir> = listOf(\n")
	for _, d := range Dirs() {
		fmt.Fprintf(&b, "        Dir(%q, %d, %v),\n", d.Key, d.Deg, d.Key == "c")
	}
	b.WriteString("    )\n\n")

	for _, name := range []string{"KINDS", "BUTTONS", "CARDS"} {
		fmt.Fprintf(&b, "    val %s: List<String> = listOf(", strings.ToLower(name))
		for i, w := range Words(name) {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%q", w)
		}
		b.WriteString(")\n")
	}
	b.WriteString("\n")

	b.WriteString("    /** Готовые виды в порядке экрана выбора. */\n")
	b.WriteString("    val looks: List<Look> = listOf(\n")
	for _, l := range LookList() {
		fmt.Fprintf(&b, "        Look(%q, %q, %q, %s, %q, %q, %q, %q, %q),\n",
			l.Preset, l.Kind, l.Dir, kotlinFloat(l.Depth), l.Radius, l.Density, l.Btn, l.Glow, l.Card)
	}
	b.WriteString("    )\n\n")

	b.WriteString("    /** Все акценты из пресетов без повторов, в порядке первого появления. */\n")
	b.WriteString("    val accents: List<Int> = listOf(\n")
	for _, c := range accents() {
		fmt.Fprintf(&b, "        0x%s.toInt(),\n", argb(c))
	}
	b.WriteString("    )\n\n")

	b.WriteString("    data class Preset(val key: String, val bg: Long, val fg: Long, val acc: Long)\n")
	b.WriteString("    data class Density(val key: String, val pad: Int, val gap: Int)\n")
	b.WriteString("    data class Radius(val key: String, val r: Int)\n")
	b.WriteString("    data class Glow(val key: String, val a: Double)\n")
	b.WriteString("    data class Dir(val key: String, val deg: Int, val centered: Boolean)\n")
	b.WriteString("    data class Look(\n")
	b.WriteString("        val preset: String, val kind: String, val dir: String, val depth: Double,\n")
	b.WriteString("        val radius: String, val density: String, val btn: String, val glow: String, val card: String,\n")
	b.WriteString("    )\n")
	b.WriteString("}\n")
	return b.String()
}

// argb переводит #rrggbb в FFrrggbb — так цвет пишется в Kotlin как Long.
func argb(hex string) string { return "FF" + strings.ToUpper(strings.TrimPrefix(hex, "#")) }

// kotlinFloat пишет число так, чтобы Kotlin прочёл его как Double.
func kotlinFloat(v float64) string {
	s := strconv.FormatFloat(v, 'f', -1, 64)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}

// accents — те же, что Look.ACCENTS в look.js: акценты пресетов без повторов.
func accents() []string {
	seen := make(map[string]bool)
	var out []string
	for _, p := range PresetList() {
		if !seen[p.Acc] {
			seen[p.Acc] = true
			out = append(out, p.Acc)
		}
	}
	return out
}

// KotlinPath — куда генератор кладёт таблицу. Относительно корня репозитория:
// go generate запускается из каталога пакета, и путь считается от него.
const KotlinPath = "../../android/app/src/main/kotlin/io/marvia/android/LookTable.kt"
