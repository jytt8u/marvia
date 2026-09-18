package io.marvia.android

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.TimeZone

/**
 * Числа на экране расхода не с чем сверить: их никто, кроме телефона, не
 * знает. Поэтому проверяем сам счёт — раскладку по дням и часам и то, что
 * запись переживает чтение.
 */
class TrafficTest {

    private val msk: TimeZone = TimeZone.getTimeZone("Europe/Moscow")

    @Test
    fun aDayIsCountedByThePhonesOwnClock() {
        // 2026-09-14T21:30Z — это уже 15 сентября, полпервого ночи, в Москве.
        val at = 1_789_421_400_000L
        assertEquals(
            "тот же миг в UTC и в Москве не должен попадать в одни сутки",
            Traffic.dayOf(at, TimeZone.getTimeZone("UTC")) + 1,
            Traffic.dayOf(at, msk),
        )
        assertEquals(0, Traffic.hourOf(at, msk))
    }

    @Test
    fun theFirstOfJanuaryNineteenSeventyWasAThursday() {
        assertEquals(3, Traffic.weekdayOf(0))
        assertEquals(4, Traffic.weekdayOf(1))
        assertEquals(0, Traffic.weekdayOf(4))
    }

    @Test
    fun daysSurviveWritingAndReading() {
        val hours = LongArray(24) { it.toLong() * 100 }
        val days = mapOf(20_000L to hours)
        val back = Traffic.decodeDays(Traffic.encodeDays(days))
        assertEquals(setOf(20_000L), back.keys)
        assertTrue(hours.contentEquals(back[20_000L]!!))
    }

    /** Старая или битая строка стоит одного дня, а не всего графика. */
    @Test
    fun aBrokenLineCostsOnlyItsOwnDay() {
        val good = "20001:" + LongArray(24) { 1 }.joinToString(",")
        val text = "мусор\n20000:1,2,3\n" + good
        val back = Traffic.decodeDays(text)
        assertEquals(setOf(20_001L), back.keys)
    }

    /** Помним тридцать суток: столько же, сколько показываем. */
    @Test
    fun onlyThirtyDaysAreKept() {
        val many = (1..40L).associate { day -> day to LongArray(24) { 1 } }
        val back = Traffic.decodeDays(Traffic.encodeDays(many))
        assertEquals(Traffic.KEEP_DAYS, back.size)
        assertEquals(11L, back.keys.min())
        assertEquals(40L, back.keys.max())
    }

    @Test
    fun placesSurviveWritingAndReading() {
        val places = mapOf("Финляндия" to 30L, "ОАЭ" to 70L)
        val back = Traffic.decodePlaces(Traffic.encodePlaces(places))
        assertEquals(places, back)
    }

    /** Имя страны с двоеточием и пробелами не должно разъезжаться при записи. */
    @Test
    fun aPlaceNameWithPunctuationStaysWhole() {
        val name = "Нидерланды · Амстердам: запас"
        val back = Traffic.decodePlaces(Traffic.encodePlaces(mapOf(name to 5L)))
        assertEquals(mapOf(name to 5L), back)
    }

    /**
     * Строки уезжают в XML настроек, а он управляющих символов не допускает.
     * Разделитель, выбранный без оглядки на это, ломает не одну запись, а весь
     * файл настроек приложения.
     */
    @Test
    fun whatIsWrittenStaysInsideWhatXmlAllows() {
        val text = Traffic.encodePlaces(mapOf("Финляндия" to 1L)) +
            Traffic.encodeDays(mapOf(20_000L to LongArray(24) { 1 }))
        for (c in text) {
            val ok = c == '\n' || c == '\t' || c.code >= 0x20
            assertTrue("недопустимый в XML символ: " + c.code, ok)
        }
    }
}
