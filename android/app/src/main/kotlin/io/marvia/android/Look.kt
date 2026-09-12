package io.marvia.android

import kotlin.math.pow
import kotlin.math.roundToInt

/**
 * Theme — цвета и размеры, посчитанные по выбору человека.
 *
 * Один объект на весь экран. Ни одна вьюха не знает про пресеты, акценты и
 * плотности — она получает готовые токены и красится ими. Так тема живёт в
 * одном месте, а экраны остаются экранами.
 *
 * Имена те же, что в look.js у панели и окна на компьютере: поле здесь —
 * переменная там. Совпадение имён не случайность, а способ сверять.
 */
data class Theme(
    val bg: Int,
    /** Свет: освещённая сторона, середина, тень. При ровном фоне все три — bg. */
    val lit: Int,
    val mid: Int,
    val shade: Int,
    val surf: Int,
    val surf2: Int,
    val line: Int,
    val fg: Int,
    val dim: Int,
    val acc: Int,
    val accFg: Int,
    val accSoft: Int,
    val warn: Int,
    val fail: Int,
    val dark: Boolean,
    /** Характер и направление света — для рисования фона. */
    val kind: String,
    val dir: String,
    /** Цвет, которым подкрашена светлая сторона: оттенок или акцент. */
    val tint: Int,
    /** Сила свечения 0…1. */
    val glowA: Double,
    /** Кнопка питания и подача карточек. */
    val btn: String,
    val card: String,
    /** Скругление, отступ карточки и зазор между карточками — в dp. */
    val r: Int,
    val pad: Int,
    val gap: Int,
)

/**
 * Look — арифметика темы. Формулы переписаны из look.js один в один:
 * смешение цветов, относительная яркость по WCAG, подбор приглушённого
 * текста до порога контраста 4.5, свет с трёх сторон. Числа таблицы приезжают
 * из LookTable — его пишет генератор, здесь только вычисления.
 */
object Look {

    /**
     * Выбор человека. accent == 0 — «акцент из пресета», tint == 0 — «как
     * акцент». Всё остальное — ключи из таблицы; незнакомый ключ normalize
     * заменяет на вид из коробки поле по полю.
     */
    data class Choice(
        val preset: String = DEFAULT.preset,
        val accent: Int = 0,
        val kind: String = DEFAULT.kind,
        val dir: String = DEFAULT.dir,
        val depth: Double = DEFAULT.depth,
        val tint: Int = 0,
        val radius: String = DEFAULT.radius,
        val density: String = DEFAULT.density,
        val btn: String = DEFAULT.btn,
        val glow: String = DEFAULT.glow,
        val card: String = DEFAULT.card,
    )

    /** Вид из коробки — первый из готовых, как в look.js. */
    val DEFAULT: LookTable.Look get() = LookTable.looks[0]

    fun preset(key: String): LookTable.Preset =
        LookTable.presets.firstOrNull { it.key == key } ?: LookTable.presets.first { it.key == DEFAULT.preset }

    fun density(key: String): LookTable.Density =
        LookTable.densities.firstOrNull { it.key == key } ?: LookTable.densities.first { it.key == DEFAULT.density }

    fun radius(key: String): Int =
        (LookTable.radii.firstOrNull { it.key == key } ?: LookTable.radii.first { it.key == DEFAULT.radius }).r

    fun glow(key: String): Double =
        (LookTable.glows.firstOrNull { it.key == key } ?: LookTable.glows.first { it.key == DEFAULT.glow }).a

    /** normalize приводит выбор к допустимому — так же, как Look.normalize в look.js. */
    fun normalize(c: Choice): Choice = Choice(
        preset = if (LookTable.presets.any { it.key == c.preset }) c.preset else DEFAULT.preset,
        accent = c.accent,
        kind = if (c.kind in LookTable.kinds) c.kind else DEFAULT.kind,
        dir = if (LookTable.dirs.any { it.key == c.dir }) c.dir else DEFAULT.dir,
        depth = if (c.depth in 0.08..0.98) (c.depth * 100).roundToInt() / 100.0 else DEFAULT.depth,
        tint = c.tint,
        radius = if (LookTable.radii.any { it.key == c.radius }) c.radius else DEFAULT.radius,
        density = if (LookTable.densities.any { it.key == c.density }) c.density else DEFAULT.density,
        btn = if (c.btn in LookTable.buttons) c.btn else DEFAULT.btn,
        glow = if (LookTable.glows.any { it.key == c.glow }) c.glow else DEFAULT.glow,
        card = if (c.card in LookTable.cards) c.card else DEFAULT.card,
    )

    /** ofLook — выбор по готовому виду: акцент и оттенок сбрасываются, у каждого свой. */
    fun ofLook(l: LookTable.Look): Choice = Choice(
        preset = l.preset, kind = l.kind, dir = l.dir, depth = l.depth,
        radius = l.radius, density = l.density, btn = l.btn, glow = l.glow, card = l.card,
    )

    /** same — совпадает ли выбор с готовым видом; акцент и оттенок не в счёт. */
    fun same(c: Choice, l: LookTable.Look): Boolean {
        val n = normalize(c)
        return n.preset == l.preset && n.kind == l.kind && n.dir == l.dir && n.depth == l.depth &&
            n.radius == l.radius && n.density == l.density && n.btn == l.btn && n.glow == l.glow && n.card == l.card
    }

    fun theme(raw: Choice): Theme {
        val choice = normalize(raw)
        val p = preset(choice.preset)
        val d = density(choice.density)
        val bg = p.bg.toInt()
        val fg = p.fg.toInt()
        val acc = if (choice.accent != 0) choice.accent else p.acc.toInt()
        val dark = lum(bg) < 0.5
        val tint = if (choice.tint != 0) choice.tint else acc

        // Свет: освещённая сторона подкрашена оттенком, тёмная — уведена в
        // чёрный на глубину тени. Ровный фон — без света: поверхности тогда
        // считаются от самого фона, как в пресете.
        val flat = choice.kind == "flat"
        val lit = if (flat) bg else mix(bg, tint, if (dark) 0.30 else 0.16)
        val shade = if (flat) bg else mix(bg, BLACK, if (dark) choice.depth else choice.depth * 0.22)
        val mid = if (flat) bg else mix(lit, shade, 0.5)

        val surf = if (dark) mix(mid, WHITE, 0.06) else mix(mid, WHITE, 0.55)
        val surf2 = if (dark) mix(mid, WHITE, 0.13) else mix(mid, BLACK, 0.07)
        return Theme(
            bg = bg,
            lit = lit,
            mid = mid,
            shade = shade,
            surf = surf,
            surf2 = surf2,
            line = if (dark) mix(mid, WHITE, 0.18) else mix(mid, BLACK, 0.14),
            fg = fg,
            dim = readableDim(fg, listOf(lit, mid, shade, surf, surf2)),
            acc = acc,
            accFg = bestOn(acc),
            accSoft = withAlpha(acc, 0.14),
            // Предупреждение и отказ одни на все темы: янтарный и красный
            // должны читаться как «внимание» независимо от акцента, иначе в
            // розовой теме ошибка сольётся с кнопкой.
            warn = 0xFFF2A03D.toInt(),
            fail = if (dark) 0xFFFF6B7A.toInt() else 0xFFC8323F.toInt(),
            dark = dark,
            kind = choice.kind,
            dir = choice.dir,
            tint = tint,
            glowA = glow(choice.glow),
            btn = choice.btn,
            card = choice.card,
            r = radius(choice.radius),
            pad = d.pad,
            gap = d.gap,
        )
    }

    // ---------------------------------------------------------- код темы

    /**
     * Код темы — весь вид одной строкой, чтобы передать другу или в другой
     * клиент. Формат тот же, что в look.js: первые три буквы пресета
     * уникальны, остальное — по две. Разошёлся бы с JavaScript — и код с
     * телефона не открылся бы в панели; тест сверяет оба на одних примерах.
     */
    fun encode(raw: Choice): String {
        val c = normalize(raw)
        val hex = { v: Int -> if (v == 0) "0" else "%06X".format(v and 0xFFFFFF) }
        return listOf(
            "MV",
            short(c.preset, 3),
            hex(c.accent),
            short(c.kind, 3) + c.dir.uppercase(),
            (c.depth * 100).roundToInt().toString(),
            short(c.radius, 2) + short(c.density, 2),
            short(c.btn, 2) + short(c.glow, 2) + short(c.card, 2),
            hex(c.tint),
        ).joinToString("-")
    }

    /** decode разбирает код; null — чужой или битый. */
    fun decode(code: String?): Choice? {
        val p = code.orEmpty().trim().uppercase().split("-")
        if (p.size != 8 || p[0] != "MV") return null
        val preset = LookTable.presets.map { it.key }.find(3, p[1]) ?: return null
        if (p[3].length < 4) return null
        val kind = LookTable.kinds.find(3, p[3].substring(0, 3)) ?: return null
        val dir = LookTable.dirs.map { it.key }.firstOrNull { it.uppercase() == p[3].substring(3) } ?: return null
        val depth = (p[4].toIntOrNull() ?: return null) / 100.0
        if (depth !in 0.08..0.98) return null
        if (p[5].length != 4 || p[6].length != 6) return null
        val radius = LookTable.radii.map { it.key }.find(2, p[5].substring(0, 2)) ?: return null
        val density = LookTable.densities.map { it.key }.find(2, p[5].substring(2)) ?: return null
        val btn = LookTable.buttons.find(2, p[6].substring(0, 2)) ?: return null
        val glow = LookTable.glows.map { it.key }.find(2, p[6].substring(2, 4)) ?: return null
        val card = LookTable.cards.find(2, p[6].substring(4)) ?: return null
        val accent = color(p[2]) ?: return null
        val tint = color(p[7]) ?: return null
        return Choice(preset, accent, kind, dir, depth, tint, radius, density, btn, glow, card)
    }

    private fun short(s: String, n: Int) = s.take(n).uppercase()
    private fun List<String>.find(n: Int, code: String) = firstOrNull { short(it, n) == code }

    /** color: «0» — нет, шесть шестнадцатеричных — цвет, остальное — брак (null). */
    private fun color(s: String): Int? = when {
        s == "0" -> 0
        Regex("^[0-9A-F]{6}$").matches(s) -> BLACK or s.toInt(16)
        else -> null
    }

    // ------------------------------------------------------- арифметика

    /** mix смешивает два цвета: t = 0 — первый, t = 1 — второй. */
    fun mix(a: Int, b: Int, t: Double): Int {
        fun ch(x: Int, y: Int) = (x + (y - x) * t).roundToInt().coerceIn(0, 255)
        return rgb(ch(red(a), red(b)), ch(green(a), green(b)), ch(blue(a), blue(b)))
    }

    fun withAlpha(c: Int, alpha: Double): Int =
        ((alpha * 255).roundToInt().coerceIn(0, 255) shl 24) or (c and 0x00FFFFFF)

    /** lum — воспринимаемая яркость: по ней тема считается тёмной или светлой. */
    fun lum(c: Int): Double =
        (0.299 * red(c) + 0.587 * green(c) + 0.114 * blue(c)) / 255.0

    /** rl — относительная яркость по WCAG, для контраста. */
    private fun rl(c: Int): Double {
        fun lin(v: Int): Double {
            val s = v / 255.0
            return if (s <= 0.03928) s / 12.92 else ((s + 0.055) / 1.055).pow(2.4)
        }
        return 0.2126 * lin(red(c)) + 0.7152 * lin(green(c)) + 0.0722 * lin(blue(c))
    }

    fun ratio(a: Int, b: Int): Double {
        val x = rl(a)
        val y = rl(b)
        return (maxOf(x, y) + 0.05) / (minOf(x, y) + 0.05)
    }

    /** bestOn — какой текст читается на этом фоне: почти чёрный или белый. */
    fun bestOn(bg: Int): Int {
        val ink = 0xFF08110D.toInt()
        return if (ratio(ink, bg) >= ratio(WHITE, bg)) ink else WHITE
    }

    /**
     * readableDim подбирает приглушённый текст: настолько блёклый, насколько
     * позволяет контраст 4.5 на каждом из фонов, где им пишут.
     */
    fun readableDim(fg: Int, grounds: List<Int>): Int {
        val tone = grounds.last()
        fun ok(c: Int) = grounds.all { ratio(c, it) >= 4.5 }
        var t = 0.55
        var out = mix(fg, tone, t)
        while (t > 0 && !ok(out)) {
            t = maxOf(0.0, t - 0.04)
            out = mix(fg, tone, t)
        }
        return out
    }

    // Свои разборщики цвета вместо android.graphics.Color: тот в обычных
    // тестах на JVM — заглушка, возвращающая нули, а формулы хочется проверять
    // без телефона. Цвет — ARGB в Int, как везде в Android.
    private const val WHITE = 0xFFFFFFFF.toInt()
    private const val BLACK = 0xFF000000.toInt()

    private fun red(c: Int) = (c shr 16) and 0xFF
    private fun green(c: Int) = (c shr 8) and 0xFF
    private fun blue(c: Int) = c and 0xFF
    private fun rgb(r: Int, g: Int, b: Int) = BLACK or (r shl 16) or (g shl 8) or b
}
