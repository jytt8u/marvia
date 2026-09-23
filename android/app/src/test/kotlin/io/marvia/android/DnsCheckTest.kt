package io.marvia.android

import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * Свой резолвер принимается только тот, до которого туннель дотянется:
 * опечатка и адрес домашней сети отвергаются до сохранения, каждый со своей
 * причиной.
 */
class DnsCheckTest {

    @Test
    fun aPublicAddressIsAccepted() {
        for (a in listOf("76.76.2.0", "1.1.1.3", "208.67.222.222", " 9.9.9.11 ")) {
            assertEquals(a, Store.DnsCheck.OK, Store.checkDns(a))
        }
    }

    @Test
    fun aTypoIsRejectedAsATypo() {
        for (a in listOf("", "1.1.1", "1.1.1.1.1", "256.1.1.1", "1.1.1.x", "01.1.1.1", "1..1.1", "dns.google", "2606:4700::1111", "1.1.1.1:53")) {
            assertEquals(a, Store.DnsCheck.BAD, Store.checkDns(a))
        }
    }

    @Test
    fun aLocalAddressIsRejectedWithItsOwnReason() {
        for (a in listOf("192.168.1.1", "10.0.0.1", "172.16.0.1", "172.31.255.255", "127.0.0.1", "169.254.1.1", "100.64.0.1", "0.0.0.0", "224.0.0.1")) {
            assertEquals(a, Store.DnsCheck.LOCAL, Store.checkDns(a))
        }
        // Соседи частных диапазонов — обычные публичные адреса.
        for (a in listOf("172.15.0.1", "172.32.0.1", "100.63.0.1", "100.128.0.1", "192.169.0.1")) {
            assertEquals(a, Store.DnsCheck.OK, Store.checkDns(a))
        }
    }
}
