package look

import (
	"fmt"
	"regexp"
	"strings"
)

// Приложение на Android не умеет читать look.js: там нет браузера, а тащить
// движок JavaScript ради таблицы из тридцати строк — безумие. Поэтому таблица
// переписывается в Kotlin генератором — из того же файла, который вшивается в
// панель и окно. Правка цвета в look.js и `go generate ./internal/look` красят
// все три клиента; тест следит, что Kotlin не отстал.

//go:generate go run ./gen

// Preset — одна тема из таблицы: фон, текст, акцент.
type Preset struct {
	Key         string
	BG, FG, Acc string
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

// Densities отдаёт плотности: радиус, отступ, зазор — в порядке таблицы.
func Densities() []Density {
	var out []Density
	for _, m := range densityRow.FindAllStringSubmatch(js, -1) {
		var d Density
		d.Key = m[1]
		fmt.Sscanf(m[2]+" "+m[3]+" "+m[4], "%d %d %d", &d.R, &d.Pad, &d.Gap)
		out = append(out, d)
	}
	return out
}

// Density — одна плотность: скругление, отступ карточки, зазор между ними.
type Density struct {
	Key         string
	R, Pad, Gap int
}

var (
	presetRow  = regexp.MustCompile(`(?m)^    ([a-z]+): +\{ bg: "(#[0-9a-f]{6})", fg: "(#[0-9a-f]{6})", acc: "(#[0-9a-f]{6})" \}`)
	densityRow = regexp.MustCompile(`(?m)^    ([a-z]+): +\{ r: (\d+), pad: (\d+), gap: (\d+) \}`)
)

// Kotlin собирает файл таблицы для приложения на Android.
//
// Только данные: арифметика темы — mix, контраст, подбор приглушённого —
// написана на Kotlin руками в Look.kt, потому что генерировать код из кода
// значило бы породить второй, нечитаемый язык. Числа же переписывать руками
// нельзя: их восемьдесят одно, и опечатка в одном не видна ни на каком экране,
// кроме экрана того, кто выбрал именно этот пресет.
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

	b.WriteString("    /** Плотности: ключ, скругление, отступ карточки, зазор — в dp. */\n")
	b.WriteString("    val densities: List<Density> = listOf(\n")
	for _, d := range Densities() {
		fmt.Fprintf(&b, "        Density(%q, %d, %d, %d),\n", d.Key, d.R, d.Pad, d.Gap)
	}
	b.WriteString("    )\n\n")

	b.WriteString("    /** Все акценты из пресетов без повторов, в порядке первого появления. */\n")
	b.WriteString("    val accents: List<Int> = listOf(\n")
	for _, c := range accents() {
		fmt.Fprintf(&b, "        0x%s.toInt(),\n", argb(c))
	}
	b.WriteString("    )\n\n")

	fmt.Fprintf(&b, "    const val DEFAULT_PRESET = %q\n", defaultPreset())
	fmt.Fprintf(&b, "    const val DEFAULT_DENSITY = %q\n", defaultDensity())
	b.WriteString("\n    data class Preset(val key: String, val bg: Long, val fg: Long, val acc: Long)\n")
	b.WriteString("    data class Density(val key: String, val r: Int, val pad: Int, val gap: Int)\n")
	b.WriteString("}\n")
	return b.String()
}

// argb переводит #rrggbb в FFrrggbb — так цвет пишется в Kotlin как Long.
func argb(hex string) string { return "FF" + strings.ToUpper(strings.TrimPrefix(hex, "#")) }

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

func defaultPreset() string  { return defaultOf("preset") }
func defaultDensity() string { return defaultOf("density") }

// defaultOf читает поле из строки DEFAULT в look.js.
func defaultOf(field string) string {
	m := regexp.MustCompile(field + `: "([a-z]+)"`).FindStringSubmatch(defaultLine())
	if m == nil {
		return ""
	}
	return m[1]
}

func defaultLine() string {
	m := regexp.MustCompile(`const DEFAULT = \{[^}]*\}`).FindString(js)
	return m
}

// KotlinPath — куда генератор кладёт таблицу. Относительно корня репозитория:
// go generate запускается из каталога пакета, и путь считается от него.
const KotlinPath = "../../android/app/src/main/kotlin/io/marvia/android/LookTable.kt"
