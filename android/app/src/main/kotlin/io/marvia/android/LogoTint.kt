package io.marvia.android

import kotlin.math.roundToInt

/**
 * LogoTint — перекраска знака «M» в цвет акцента с сохранением светотени.
 *
 * Знак нарисован как металлическая лента: у неё есть грани, блики и тени,
 * и именно они делают его знаком, а не буквой. Обычная тонировка
 * (imageTintList, SRC_IN) заливает всё одним цветом и оставляет плоское
 * пятно. Поэтому красим иначе: светлота исходного пикселя становится маской
 * освещения, а сам цвет берём из палитры трёх тонов акцента — тень, середина,
 * блик. Грань, что была светлее, останется светлее; край, что был прозрачным,
 * останется прозрачным.
 *
 * Здесь только арифметика над массивом пикселей ARGB: без Bitmap и без
 * Android, чтобы её можно было проверить обычным тестом на JVM. Bitmap
 * заворачивает и разворачивает [MarviaLogoView].
 */
object LogoTint {

    /**
     * key — ключ кэша по акценту. Ноль занят оригиналом, поэтому чёрный
     * акцент получает единицу: разница в один шаг синего глазом не видна.
     */
    fun key(acc: Int): Int = (acc and 0xFFFFFF).let { if (it == 0) 1 else it }

    /** Три тона акцента: тень, середина, блик. */
    data class Palette(val shadow: Int, val mid: Int, val highlight: Int)

    fun palette(acc: Int): Palette = Palette(
        shadow = mix(acc, 0x000000, 0.52),
        mid = acc,
        highlight = mix(acc, 0xFFFFFF, 0.58),
    )

    /**
     * tint возвращает новый массив тех же размеров, где каждый пиксель окрашен
     * по своей светлоте. Альфа переносится как есть.
     *
     * Светлота нормируется по самой тёмной и самой светлой непрозрачной точке
     * знака, а не по шкале 0…1: в исходнике нет ни чистого чёрного, ни чистого
     * белого, и без нормировки все грани сбились бы в середину палитры и
     * перестали бы отличаться. Полупрозрачные края в нормировку не берём —
     * там светлота уже смешана с фоном.
     */
    fun tint(pixels: IntArray, acc: Int): IntArray {
        val p = palette(acc)
        var lo = 1.0
        var hi = 0.0
        for (c in pixels) {
            if (alpha(c) < 128) continue
            val l = luminance(c)
            if (l < lo) lo = l
            if (l > hi) hi = l
        }
        // Знак из одного тона (или пустой) — нормировать нечего, берём шкалу целиком.
        if (hi - lo < 0.05) { lo = 0.0; hi = 1.0 }
        val span = hi - lo

        val out = IntArray(pixels.size)
        for (i in pixels.indices) {
            val c = pixels[i]
            val a = alpha(c)
            if (a == 0) continue
            val l = ((luminance(c) - lo) / span).coerceIn(0.0, 1.0)
            val rgb = if (l < 0.5) mix(p.shadow, p.mid, l * 2) else mix(p.mid, p.highlight, (l - 0.5) * 2)
            out[i] = (a shl 24) or rgb(rgb)
        }
        return out
    }

    /** Светлота 0…1 по тем же весам, что в Look: глаз видит зелёный ярче синего. */
    fun luminance(c: Int): Double =
        (0.299 * ((c shr 16) and 0xFF) + 0.587 * ((c shr 8) and 0xFF) + 0.114 * (c and 0xFF)) / 255.0

    private fun alpha(c: Int): Int = (c ushr 24) and 0xFF
    private fun rgb(c: Int): Int = c and 0xFFFFFF

    private fun mix(a: Int, b: Int, t: Double): Int {
        fun ch(shift: Int): Int {
            val x = (a shr shift) and 0xFF
            val y = (b shr shift) and 0xFF
            return (x + (y - x) * t).roundToInt().coerceIn(0, 255)
        }
        return (ch(16) shl 16) or (ch(8) shl 8) or ch(0)
    }
}
