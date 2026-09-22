package io.marvia.android

import org.junit.Assert.assertEquals
import org.junit.Test

/** Кадр своего фона переживает перезапуск и не ломается от чужой строки. */
class BackdropFrameTest {

    @Test
    fun theChosenFrameSurvivesARestart() {
        val frame = BackdropFrame(zoom = 2.5f, fx = 0.3f, fy = 0.7f, rot = 3)
        assertEquals(frame, BackdropFrame.decode(frame.encode()))
    }

    @Test
    fun aBrokenOrForeignFrameFallsBackToTheWholePhoto() {
        for (raw in listOf(null, "", "мусор", "1,2", "x,y,z,w")) {
            val f = BackdropFrame.decode(raw)
            assertEquals(1f, f.zoom)
            assertEquals(0, f.rot)
        }
        // Сохранённое чужой версией за пределами — прижимается, а не рушит фон.
        val wild = BackdropFrame.decode("99,-3,7,-1")
        assertEquals(BackdropFrame.MAX_ZOOM, wild.zoom)
        assertEquals(0f, wild.fx)
        assertEquals(1f, wild.fy)
        assertEquals(3, wild.rot)
    }
}
