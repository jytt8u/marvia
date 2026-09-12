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
    /** Скругление, отступ карточки и зазор между карточками — в dp. */
    val r: Int,
    val pad: Int,
    val gap: Int,
)

/**
 * Look — арифметика темы. Формулы переписаны из look.js один в один:
 * смешение цветов, относительная яркость по WCAG и подбор приглушённого
 * текста до порога контраста 4.5. Числа таблицы приезжают из LookTable —
 * его пишет генератор, здесь только вычисления.
 */
object Look {

    /** Выбор человека. accent == 0 означает «акцент из пресета». */
    data class Choice(
        val preset: String = LookTable.DEFAULT_PRESET,
        val accent: Int = 0,
        val density: String = LookTable.DEFAULT_DENSITY,
    )

    fun preset(key: String): LookTable.Preset =
        LookTable.presets.firstOrNull { it.key == key }
            ?: LookTable.presets.first { it.key == LookTable.DEFAULT_PRESET }

    fun density(key: String): LookTable.Density =
        LookTable.densities.firstOrNull { it.key == key }
            ?: LookTable.densities.first { it.key == LookTable.DEFAULT_DENSITY }

    fun theme(choice: Choice): Theme {
        val p = preset(choice.preset)
        val d = density(choice.density)
        val bg = p.bg.toInt()
        val fg = p.fg.toInt()
        val acc = if (choice.accent != 0) choice.accent else p.acc.toInt()
        val dark = lum(bg) < 0.5
        val toward = if (dark) WHITE else BLACK
        val surf = mix(bg, toward, 0.05)
        val surf2 = mix(bg, toward, 0.10)
        return Theme(
            bg = bg,
            surf = surf,
            surf2 = surf2,
            line = mix(bg, toward, 0.16),
            fg = fg,
            dim = readableDim(fg, listOf(surf, surf2)),
            acc = acc,
            accFg = bestOn(acc),
            accSoft = withAlpha(acc, 0.13),
            // Предупреждение и отказ одни на все темы: янтарный и красный
            // должны читаться как «внимание» независимо от акцента, иначе в
            // розовой теме ошибка сольётся с кнопкой.
            warn = 0xFFF2A03D.toInt(),
            fail = if (dark) 0xFFFF6B7A.toInt() else 0xFFC8323F.toInt(),
            dark = dark,
            r = d.r,
            pad = d.pad,
            gap = d.gap,
        )
    }

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
