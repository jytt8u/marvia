package io.marvia.android

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Учёт трафика пишет на диск редко, но не теряет: опрос раз в две секунды не
 * переписывает файл, минута, смена часа и остановка туннеля — переписывают.
 */
class LedgerTest {

    private fun fresh() = Traffic.Ledger(mutableMapOf(), mutableMapOf(), 0)

    @Test
    fun pollingEveryTwoSecondsDoesNotRewriteTheFileEachTime() {
        val l = fresh()
        l.note(1_000, "Финляндия", day = 100, hour = 10)
        assertTrue("первое — сразу, иначе после выгрузки не было бы ничего", l.due(0, force = false))
        l.written(0)
        var writes = 0
        for (i in 1..29) {
            l.note(1_000L + i * 500, "Финляндия", day = 100, hour = 10)
            val now = i * 2_000L
            if (l.due(now, force = false)) { writes++; l.written(now) }
        }
        assertEquals("за минуту опросов — ни одной лишней записи", 0, writes)
        l.note(20_000, "Финляндия", day = 100, hour = 10)
        assertTrue("прошла минута — пора", l.due(60_000, force = false))
    }

    @Test
    fun aNewHourAndAStoppedTunnelAreWrittenAtOnce() {
        val l = fresh()
        l.note(1_000, "", day = 100, hour = 10)
        l.written(0)
        l.note(2_000, "", day = 100, hour = 11)
        assertTrue("смена часа не ждёт минуты", l.due(5_000, force = false))
        l.written(5_000)
        l.note(3_000, "", day = 100, hour = 11)
        assertFalse(l.due(6_000, force = false))
        assertTrue("остановка туннеля пишет хвост", l.due(6_000, force = true))
    }

    @Test
    fun bytesLandInTheirHourAndCountryAcrossReconnects() {
        val l = fresh()
        l.note(1_000, "Финляндия", day = 100, hour = 10)
        l.note(1_500, "Финляндия", day = 100, hour = 10)
        // Туннель подняли заново — счётчик ядра начался с нуля.
        l.note(300, "ОАЭ", day = 100, hour = 11)
        assertEquals(1_500L, l.days[100]!![10])
        assertEquals(300L, l.days[100]!![11])
        assertEquals(1_500L, l.places["Финляндия"])
        assertEquals(300L, l.places["ОАЭ"])
        assertFalse("нечего писать — не пишем", fresh().due(100_000, force = true))
    }
}
