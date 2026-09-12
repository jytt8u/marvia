package io.marvia.android

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Арифметика темы на Kotlin обязана давать те же цвета, что look.js.
 *
 * Эталоны сняты с движка JavaScript в браузере (Look.theme без акцента).
 * Разошлись — телефон красится не так, как панель и окно на компьютере, и
 * заметить это глазами почти нельзя: разница в один шаг смешения.
 */
class LookTest {

    private fun hex(c: Int) = "#%06x".format(c and 0xFFFFFF)

    private fun check(preset: String, surf: String, surf2: String, line: String, dim: String, accFg: String, dark: Boolean) {
        val t = Look.theme(Look.Choice(preset = preset))
        assertEquals("$preset surf", surf, hex(t.surf))
        assertEquals("$preset surf2", surf2, hex(t.surf2))
        assertEquals("$preset line", line, hex(t.line))
        assertEquals("$preset dim", dim, hex(t.dim))
        assertEquals("$preset accFg", accFg, hex(t.accFg))
        assertEquals("$preset dark", dark, t.dark)
    }

    @Test
    fun darkPresetsMatchTheJavaScriptEngine() {
        check("steel", "#181a1d", "#242629", "#333537", "#919396", "#08110d", true)
        check("emerald", "#121c18", "#1f2824", "#2e3633", "#8a9590", "#08110d", true)
        check("oled", "#0d0d0d", "#1a1a1a", "#292929", "#8a8a8a", "#08110d", true)
        check("crimson", "#1f1214", "#2b1e21", "#392d30", "#998689", "#08110d", true)
        check("mono", "#161616", "#232323", "#313131", "#8c8c8c", "#08110d", true)
        check("gold", "#1b1912", "#27251e", "#35342d", "#959183", "#08110d", true)
    }

    @Test
    fun lightPresetsMatchTheJavaScriptEngine() {
        check("paper", "#ebe8e2", "#dedcd6", "#cfcdc8", "#63615c", "#ffffff", false)
        check("daylight", "#e7e9eb", "#dbddde", "#cccecf", "#5d6062", "#ffffff", false)
    }

    @Test
    fun accentOverridesThePresetAndKeepsReadableText() {
        val t = Look.theme(Look.Choice(preset = "steel", accent = 0xFF1FD18D.toInt()))
        assertEquals("#1fd18d", hex(t.acc))
        assertEquals("#08110d", hex(t.accFg))
    }

    @Test
    fun mixAndContrastAgreeWithTheEngine() {
        assertEquals("#121c18", hex(Look.mix(0xFF06100C.toInt(), 0xFFFFFFFF.toInt(), 0.05)))
        assertEquals(21.0, Look.ratio(0xFFFFFFFF.toInt(), 0xFF000000.toInt()), 0.001)
    }

    @Test
    fun dimTextStaysReadableOnEverySurfaceOfEveryPreset() {
        for (p in LookTable.presets) {
            val t = Look.theme(Look.Choice(preset = p.key))
            assertTrue("${p.key}: dim на surf", Look.ratio(t.dim, t.surf) >= 4.5)
            assertTrue("${p.key}: dim на surf2", Look.ratio(t.dim, t.surf2) >= 4.5)
        }
    }

    @Test
    fun unknownChoiceFallsBackToTheDefaultLook() {
        val t = Look.theme(Look.Choice(preset = "nope", density = "huge"))
        val d = Look.theme(Look.Choice())
        assertEquals(d, t)
    }
}
