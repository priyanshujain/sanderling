package app.folio.util

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull

class ParseCentsTest {
    @Test
    fun parsesEverydayAmountsExactly() {
        assertEquals(1L, parseCents("0.01"))
        assertEquals(1234L, parseCents("12.34"))
        assertEquals(1250L, parseCents("12.5"))
        assertEquals(1200L, parseCents("12.0"))
        assertEquals(100000L, parseCents("1000"))
        assertEquals(123456L, parseCents("1,234.56"))
    }

    @Test
    fun rejectsEighteenDigitWholeThatWrapsToAPositiveLong() {
        assertNull(parseCents("999999999999999999"))
    }

    @Test
    fun rejectsSeventeenDigitWholeThatWrapsToANegativeLong() {
        assertNull(parseCents("99999999999999999"))
    }

    @Test
    fun rejectsNineteenDigitWholeThatNoLongerFitsALong() {
        assertNull(parseCents("9999999999999999999"))
    }

    @Test
    fun acceptsTheLargestRepresentableAmountAndRejectsOneCentMore() {
        assertEquals(Long.MAX_VALUE, parseCents("92233720368547758.07"))
        assertNull(parseCents("92233720368547758.08"))
    }
}
