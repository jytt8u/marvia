package io.marvia.android

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/**
 * Подписки лежат в настройках строками, и разбор этих строк — единственное
 * место, где имя может потеряться. Проверяем обещание, а не устройство:
 * записанное читается обратно тем же самым.
 */
class SubscriptionTest {

    private val link = "marvia://Ml7qGm1KQIvE0aJ7t2s9nYbPz4uXwV6cRh8dLkFsTgA@panel.example/sub/aaa"

    @Test
    fun aSubscriptionSurvivesWritingAndReading() {
        val was = Store.Subscription("Продавец", link)
        assertEquals(was, Store.subscriptionOf(Store.rowOf(was)))
    }

    @Test
    fun aNameWithSpacesAndColonsStaysWhole() {
        val was = Store.Subscription("Вася: запас", link)
        assertEquals(was, Store.subscriptionOf(Store.rowOf(was)))
    }

    /**
     * Старая или битая запись не должна ронять весь список: человек с двумя
     * подписками потерял бы обе из-за одной.
     */
    @Test
    fun aRowWithoutANameIsSkipped() {
        assertNull(Store.subscriptionOf(link))
        assertNull(Store.subscriptionOf(""))
        assertNull(Store.subscriptionOf("\n" + link))
    }

    @Test
    fun withoutANameTheHostOfThePanelBecomesOne() {
        assertEquals("panel.example", Store.nameFromLink(link, "Подписка"))
    }

    @Test
    fun theHostLosesItsPort() {
        assertEquals(
            "panel.example",
            Store.nameFromLink("marvia://key@panel.example:8443/sub/aaa", "Подписка"),
        )
    }

    @Test
    fun withoutAHostTheFallbackIsUsed() {
        assertEquals("Подписка", Store.nameFromLink("ерунда", "Подписка"))
    }
}
