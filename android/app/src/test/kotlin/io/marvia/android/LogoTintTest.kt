package io.marvia.android

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Перекраска знака обязана оставить его знаком: с гранями, бликами и
 * прозрачными краями. Проверяем на маленькой «ленте» из четырёх тонов серого,
 * потому что настоящий PNG — те же тона, только много.
 */
class LogoTintTest {

    private fun argb(a: Int, rgb: Int) = (a shl 24) or rgb

    // Тень, две грани и блик — как у металлической ленты, плюс два края:
    // прозрачный и полупрозрачный.
    private val strip = intArrayOf(
        argb(255, 0x3a3d44), argb(255, 0x6b7280), argb(255, 0x9aa3b1), argb(255, 0xdfe3ea),
        argb(0, 0x000000), argb(96, 0x9aa3b1),
    )

    private fun alpha(c: Int) = (c ushr 24) and 0xFF

    @Test
    fun transparentEdgesStayTransparentAndSemiTransparentKeepTheirAlpha() {
        val out = LogoTint.tint(strip, 0x1FD18D)
        assertEquals(0, out[4])
        assertEquals(96, alpha(out[5]))
        for (i in 0 until 4) assertEquals("пиксель $i", 255, alpha(out[i]))
    }

    @Test
    fun lighterFacesStayLighterAfterTinting() {
        val out = LogoTint.tint(strip, 0x1FD18D)
        val l = (0 until 4).map { LogoTint.luminance(out[it]) }
        for (i in 0 until 3) assertTrue("грань $i светлее ${i + 1}: $l", l[i] < l[i + 1])
        // Тень темнее самого акцента, блик — светлее: три тона, а не заливка.
        val acc = LogoTint.luminance(0x1FD18D)
        assertTrue(l[0] < acc)
        assertTrue(l[3] > acc)
    }

    @Test
    fun theTintCarriesTheAccentHue() {
        val out = LogoTint.tint(strip, 0x1FD18D)
        for (i in 0 until 4) {
            val r = (out[i] shr 16) and 0xFF
            val g = (out[i] shr 8) and 0xFF
            val b = out[i] and 0xFF
            assertTrue("зелёный канал ведёт в пикселе $i", g > r && g > b)
        }
    }

    @Test
    fun lightThemeAccentStillGivesShadowMidAndHighlight() {
        // «Бумага»: тёмно-коричневый акцент на светлом фоне.
        val p = LogoTint.palette(0x8A4B1F)
        assertTrue(LogoTint.luminance(p.shadow) < LogoTint.luminance(p.mid))
        assertTrue(LogoTint.luminance(p.mid) < LogoTint.luminance(p.highlight))
        val out = LogoTint.tint(strip, 0x8A4B1F)
        assertTrue(LogoTint.luminance(out[0]) < LogoTint.luminance(out[3]))
    }

    @Test
    fun stockGreyAccentMeansTheOriginalPicture() {
        assertTrue(LogoTint.isStock(LogoTint.STOCK_ACCENT))
        assertTrue(LogoTint.isStock(LogoTint.STOCK_ACCENT or (0xFF shl 24)))
        assertFalse(LogoTint.isStock(0x1FD18D))
    }

    @Test
    fun aFlatPictureDoesNotBlowUpTheNormalisation() {
        val flat = intArrayOf(argb(255, 0x808080), argb(255, 0x808080))
        val out = LogoTint.tint(flat, 0x1FD18D)
        assertEquals(out[0], out[1])
        assertEquals(255, alpha(out[0]))
    }
}
