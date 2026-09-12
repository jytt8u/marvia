package io.marvia.android

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Арифметика темы на Kotlin обязана давать те же цвета, что look.js.
 *
 * Эталоны сняты с движка JavaScript (Look.theme({preset}) — вид из коробки с
 * другим пресетом). Разошлись — телефон красится не так, как панель и окно
 * на компьютере, и заметить это глазами почти нельзя: разница в один шаг
 * смешения.
 */
class LookTest {

    private fun hex(c: Int) = "#%06x".format(c and 0xFFFFFF)

    private fun check(
        preset: String, lit: String, mid: String, shade: String,
        surf: String, surf2: String, line: String, dim: String, accFg: String, dark: Boolean,
    ) {
        val t = Look.theme(Look.Choice(preset = preset))
        assertEquals("$preset lit", lit, hex(t.lit))
        assertEquals("$preset mid", mid, hex(t.mid))
        assertEquals("$preset shade", shade, hex(t.shade))
        assertEquals("$preset surf", surf, hex(t.surf))
        assertEquals("$preset surf2", surf2, hex(t.surf2))
        assertEquals("$preset line", line, hex(t.line))
        assertEquals("$preset dim", dim, hex(t.dim))
        assertEquals("$preset accFg", accFg, hex(t.accFg))
        assertEquals("$preset dark", dark, t.dark)
    }

    @Test
    fun darkPresetsMatchTheJavaScriptEngine() {
        check("steel", "#43474c", "#24272a", "#050608", "#313437", "#404346", "#4b4e50", "#b3b6b9", "#08110d", true)
        check("emerald", "#0e4a33", "#09291c", "#030705", "#18362a", "#29453a", "#355045", "#9eb1a8", "#08110d", true)
        check("oled", "#124d3a", "#09271d", "#000000", "#18342b", "#29433a", "#354e46", "#acb6b2", "#08110d", true)
        check("crimson", "#5a151e", "#320c11", "#090204", "#3e1b1f", "#4d2c30", "#57383c", "#b79c9f", "#08110d", true)
        check("mono", "#525252", "#2c2c2c", "#050505", "#393939", "#474747", "#525252", "#cacaca", "#08110d", true)
        check("gold", "#50431f", "#2c2511", "#070602", "#39321f", "#474130", "#524c3c", "#b9b3a0", "#08110d", true)
    }

    @Test
    fun lightPresetsMatchTheJavaScriptEngine() {
        check("paper", "#e6d9cd", "#e0d8cf", "#d9d6d1", "#f1ede9", "#d0c9c1", "#c1bab2", "#56524d", "#ffffff", false)
        check("daylight", "#cddfdb", "#d2dbda", "#d6d7d9", "#ebefee", "#c3cccb", "#b5bcbb", "#4d5253", "#ffffff", false)
    }

    @Test
    fun flatLightAndAuroraMatchTheJavaScriptEngine() {
        val flat = Look.theme(Look.Choice(preset = "ultra", kind = "flat"))
        assertEquals("#141528", hex(flat.surf))
        assertEquals("#262638", hex(flat.surf2))
        assertEquals("#323343", hex(flat.line))
        assertEquals("#9294a9", hex(flat.dim))
        assertEquals(flat.bg, flat.lit)
        assertEquals(flat.bg, flat.shade)

        val aurora = Look.theme(Look.Choice(preset = "plum", kind = "aurora", dir = "se", depth = 0.8, tint = 0xFF54C8FF.toInt()))
        assertEquals("#21415b", hex(aurora.lit))
        assertEquals("#122130", hex(aurora.mid))
        assertEquals("#020104", hex(aurora.shade))
        assertEquals("#202e3c", hex(aurora.surf))
        assertEquals("#adacbd", hex(aurora.dim))
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
    fun dimTextStaysReadableOnEverySurfaceOfEveryLook() {
        for (l in LookTable.looks) {
            val t = Look.theme(Look.ofLook(l))
            for ((name, ground) in listOf("lit" to t.lit, "mid" to t.mid, "shade" to t.shade, "surf" to t.surf, "surf2" to t.surf2)) {
                assertTrue("${l.preset}/${l.kind}: dim на $name", Look.ratio(t.dim, ground) >= 4.5)
            }
        }
    }

    @Test
    fun unknownChoiceFallsBackToTheDefaultLook() {
        val t = Look.theme(Look.Choice(preset = "nope", density = "huge", kind = "neon", dir = "up", depth = 3.0, radius = "x", btn = "y", glow = "z", card = "w"))
        val d = Look.theme(Look.Choice())
        assertEquals(d, t)
    }

    // Код темы — тот же, что в look.js, буква в букву: код с телефона
    // должен открываться в панели и наоборот. Эталоны сняты с JavaScript.
    @Test
    fun themeCodeMatchesTheJavaScriptEngine() {
        assertEquals("MV-STE-0-LINNW-55-SONO-RISOOU-0", Look.encode(Look.Choice()))
        val fancy = Look.Choice(
            preset = "plum", accent = 0xFFFF5ECD.toInt(), tint = 0xFF54C8FF.toInt(), kind = "aurora", dir = "c",
            depth = 0.5, radius = "pill", density = "roomy", btn = "glass", glow = "neon", card = "shadow",
        )
        assertEquals("MV-PLU-FF5ECD-AURC-50-PIRO-GLNESH-54C8FF", Look.encode(fancy))
        assertEquals(fancy, Look.decode("MV-PLU-FF5ECD-AURC-50-PIRO-GLNESH-54C8FF"))
        assertEquals(fancy, Look.decode("  mv-plu-ff5ecd-aurc-50-piro-glnesh-54c8ff \n"))
    }

    @Test
    fun everyReadyLookSurvivesTheCodeRoundTrip() {
        for (l in LookTable.looks) {
            val c = Look.ofLook(l)
            assertEquals(l.preset, c, Look.decode(Look.encode(c)))
        }
    }

    @Test
    fun foreignCodeIsRefusedNotRepainted() {
        for (bad in listOf("", "garbage", "MV-XXX-0-LINNW-55-SONO-RISOOU-0", "MV-STE-0-LINNW-99-SONO-RISOOU-0",
            "MV-STE-ZZZZZZ-LINNW-55-SONO-RISOOU-0", "MV-STE-0-LINQQ-55-SONO-RISOOU-0", "MV-STE-0-LINNW-55-SONO-RISOOU")) {
            assertNull(bad, Look.decode(bad))
        }
    }

    @Test
    fun presetKeysAreUniqueByFirstThreeLetters() {
        val short = LookTable.presets.map { it.key.take(3) }
        assertEquals(short.size, short.toSet().size)
    }
}
