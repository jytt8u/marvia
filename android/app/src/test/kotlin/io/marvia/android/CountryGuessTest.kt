package io.marvia.android

import org.junit.Assert.assertEquals
import org.junit.Test

class CountryGuessTest {
    @Test fun flagAndCityIdentifyForeignServers() {
        assertEquals("Нидерланды", CountryGuess.fromName("🇳🇱 Amsterdam #3"))
        assertEquals("Финляндия", CountryGuess.fromName("Helsinki 01"))
        assertEquals("ОАЭ", CountryGuess.fromName("Dubai premium"))
    }

    @Test fun explicitCountryAndCodeWorkWithoutNetwork() {
        assertEquals("Германия", CountryGuess.fromName("Germany · Frankfurt"))
        assertEquals("США", CountryGuess.fromName("US-01"))
        assertEquals("Финляндия", CountryGuess.fromName("fi-1"))
        assertEquals("Нидерланды", CountryGuess.fromName("[NL] fast"))
        assertEquals("🇫🇮", Flags.of("FI"))
    }

    @Test fun unknownNamesAreNotPresentedAsCountries() {
        assertEquals("", CountryGuess.fromName("My private CDN"))
        assertEquals("", CountryGuess.fromName("server-03"))
        assertEquals("", CountryGuess.fromName("business proxy"))
        assertEquals("", CountryGuess.fromName("marvia-185"))
    }

    @Test fun sellerCountryRemainsAuthoritativeAndCoreNameDoesNotChange() {
        val row = NodeRow(1, "🇳🇱 Amsterdam", "Германия · Берлин", 0, false, false, false)
        assertEquals("Германия", row.group)
        assertEquals("Германия · Берлин · 🇳🇱 Amsterdam", row.title)
        val foreign = row.copy(country = "")
        assertEquals("Нидерланды", foreign.group)
        assertEquals("🇳🇱 Amsterdam", foreign.title)
        val learned = NodeRow(2, "marvia-185", "", 0, true, true, false, exitCountry = "Германия")
        assertEquals("Германия", learned.group)
        assertEquals(true, learned.countryFromExit)
        assertEquals("Финляндия", learned.copy(country = "Финляндия").group)
        assertEquals(false, learned.copy(country = "Финляндия").countryFromExit)
    }
}
