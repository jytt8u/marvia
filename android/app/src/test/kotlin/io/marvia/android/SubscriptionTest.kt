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

    @Test
    fun aForeignNodeIsCalledByItsOwnName() {
        assertEquals(
            "🇳🇱 Amsterdam",
            Store.nameFromLink("vless://id@nl.example.com:443?security=reality#%F0%9F%87%B3%F0%9F%87%B1%20Amsterdam", "Подписка"),
        )
        assertEquals("nl.example.com", Store.nameFromLink("trojan://p@nl.example.com:443", "Подписка"))
    }

    @Test
    fun aForeignSubscriptionIsCalledByItsPanel() {
        assertEquals("sub.example.org", Store.nameFromLink("https://sub.example.org:2096/sub/abc?format=v2ray", "Подписка"))
    }
}
