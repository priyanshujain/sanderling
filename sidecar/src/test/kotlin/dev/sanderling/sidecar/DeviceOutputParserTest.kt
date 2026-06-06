package dev.sanderling.sidecar

import org.junit.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull

class DeviceOutputParserTest {

    // /proc/pid/stat: the comm field is parenthesized and may itself contain
    // spaces and a ')'. Splitting before substringAfterLast(')') would shift
    // every field index and read the wrong utime/stime ticks.
    @Test fun parseCpuTicksSumsUtimeAndStimeAfterComm() {
        val cases = listOf(
            statLine("(app)", utime = 100, stime = 23) to 123L,
            statLine("(com.foo (bar))", utime = 7, stime = 8) to 15L,
            statLine("(weird )name)", utime = 1, stime = 2) to 3L,
        )
        for ((line, expected) in cases) {
            assertEquals(expected, parseCpuTicks(line), line)
        }
    }

    @Test fun parseCpuTicksReturnsNullOnTruncatedOrNonNumericStat() {
        assertNull(parseCpuTicks("1234 (app) S 1 2 3"))
        assertNull(parseCpuTicks("1234 (app) S " + (1..12).joinToString(" ") { "x" }))
        assertNull(parseCpuTicks(""))
    }

    // VmRSS/VmSize lines are "Key:\t<number> kB"; the kB unit token must not be
    // read as the value, and a missing value must not crash the sampler.
    @Test fun parseKbReadsSecondFieldOrNull() {
        assertEquals(2048L, parseKb("VmRSS:\t  2048 kB"))
        assertEquals(900100L, parseKb("VmSize: 900100 kB"))
        assertNull(parseKb("VmRSS:"))
        assertNull(parseKb("VmRSS: notanumber kB"))
    }

    @Test fun pngWidthAndHeightReadIhdrDimensions() {
        val png = ihdr(width = 1080, height = 2340)
        assertEquals(1080, pngWidth(png))
        assertEquals(2340, pngHeight(png))
    }

    // A short/empty screencap (the device returned nothing) must report 0
    // rather than indexing past the buffer.
    @Test fun pngWidthAndHeightReturnZeroOnTruncatedInput() {
        assertEquals(0, pngWidth(ByteArray(23)))
        assertEquals(0, pngHeight(ByteArray(23)))
        assertEquals(0, pngWidth(ByteArray(0)))
    }

    @Test fun parseBoundsAcceptsWellFormedAndRejectsMalformed() {
        assertEquals(listOf(0, 0, 1080, 2340), parseBounds("[0,0,1080,2340]")?.toList())
        assertEquals(listOf(-5, -10, 20, 30), parseBounds("[-5,-10,20,30]")?.toList())
        assertNull(parseBounds("[0,0,1080]"))
        assertNull(parseBounds("0,0,1,1"))
        assertNull(parseBounds("[0, 0, 1, 1]"))
        assertNull(parseBounds(""))
    }

    @Test fun findBoundsBySelectorMatchesIdSuffixForm() {
        val tree = node(
            "resource-id" to "com.example:id/loginButton",
            "bounds" to "[10,20,110,80]",
        )
        assertEquals(listOf(10, 20, 110, 80), findBoundsBySelector(tree, "id:loginButton")?.toList())
        assertEquals(
            listOf(10, 20, 110, 80),
            findBoundsBySelector(tree, "id:com.example:id/loginButton")?.toList(),
        )
    }

    @Test fun findBoundsBySelectorMatchesTextAndDescPrefixDeepInTree() {
        val tree = node(
            "resource-id" to "root",
            children = listOf(
                node("text" to "Sign in", "bounds" to "[1,2,3,4]"),
                node("content-desc" to "AccountCardRow-7", "bounds" to "[5,6,7,8]"),
            ),
        )
        assertEquals(listOf(1, 2, 3, 4), findBoundsBySelector(tree, "text:Sign in")?.toList())
        assertEquals(listOf(5, 6, 7, 8), findBoundsBySelector(tree, "descPrefix:AccountCard")?.toList())
    }

    @Test fun findBoundsBySelectorReturnsNullForBadSelectorOrNoMatch() {
        val tree = node("resource-id" to "com.example:id/x", "bounds" to "[0,0,1,1]")
        assertNull(findBoundsBySelector(tree, "id"))
        assertNull(findBoundsBySelector(tree, "id:missing"))
    }

    @Test fun findBoundsBySelectorReturnsNullWhenMatchHasMalformedBounds() {
        val tree = node("resource-id" to "com.example:id/x", "bounds" to "not-bounds")
        assertNull(findBoundsBySelector(tree, "id:x"))
    }

    private fun statLine(comm: String, utime: Int, stime: Int): String {
        // After comm, parseCpuTicks reads index 11 (utime) and 12 (stime), so
        // the state field plus ten placeholders must precede them.
        val before = "1234 $comm S " + (1..10).joinToString(" ")
        return "$before $utime $stime 0 0 0 0"
    }

    private fun ihdr(width: Int, height: Int): ByteArray {
        val b = ByteArray(33)
        for (i in 0 until 8) b[8 + i] = 0
        b[12] = 'I'.code.toByte(); b[13] = 'H'.code.toByte()
        b[14] = 'D'.code.toByte(); b[15] = 'R'.code.toByte()
        b[16] = (width ushr 24).toByte(); b[17] = (width ushr 16).toByte()
        b[18] = (width ushr 8).toByte(); b[19] = width.toByte()
        b[20] = (height ushr 24).toByte(); b[21] = (height ushr 16).toByte()
        b[22] = (height ushr 8).toByte(); b[23] = height.toByte()
        return b
    }

    private fun node(
        vararg attrs: Pair<String, String>,
        children: List<maestro.TreeNode> = emptyList(),
    ): maestro.TreeNode = maestro.TreeNode(attributes = attrs.toMap().toMutableMap(), children = children)
}
