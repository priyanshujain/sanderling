package dev.sanderling.sidecar

import org.junit.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertTrue

class InputTextTest {

    // The fast `adb input text` path handles shell-safe ASCII of any length;
    // unicode, injection payloads, and whitespace must fall back to the driver,
    // which types them correctly. The overflow-length string (4096 a's) is pure
    // ASCII and MUST take the fast path: the per-character driver path takes
    // ~120s for it, blowing the RPC deadline and leaving focus unguarded long
    // enough for keystrokes to spray into the launcher search box. A regression
    // here would corrupt edge-case input or shell-inject the device.
    @Test fun fastInputPathAcceptsShellSafeAsciiOfAnyLength() {
        for (safe in listOf("demo@folio.app", "ledger123", "Checking", "1e10", "0.0000001", "42", "a".repeat(4096))) {
            assertTrue(FAST_INPUT_SAFE.matches(safe), "expected fast path for length ${safe.length}")
        }
        val fallback = listOf(
            "Emergency Fund", "🙂🔥💸", "  ", "\t\n", "'; DROP TABLE--",
            "<script>alert(1)</script>", "../../etc/passwd", "%s%n", "",
            "-1", "-rf", // a leading dash could be read as an option by `input text`
        )
        for (text in fallback) {
            assertTrue(!FAST_INPUT_SAFE.matches(text), "expected driver fallback for: $text")
        }
    }

    // chunkForInput must split long input but never start a chunk with '-',
    // which `input text` would read as an option flag.
    @Test fun chunkForInputSplitsToSizeAndReassembles() {
        val text = "a".repeat(4096)
        val chunks = chunkForInput(text, 512)
        assertEquals(text, chunks.joinToString(""))
        assertTrue(chunks.all { it.length <= 512 }, "no chunk may exceed the size")
        assertTrue(chunks.size >= 8, "4096/512 should be at least 8 chunks")
    }

    @Test fun chunkForInputNeverStartsAChunkWithDash() {
        // a boundary that would fall on '-' is pushed past the dashes
        val chunks = chunkForInput("ab--cd", 2)
        assertEquals("ab--cd", chunks.joinToString(""))
        assertTrue(chunks.drop(1).none { it.startsWith("-") }, "no later chunk may start with '-'")
    }

    // A logical key name must map to the right Android keycode; a typo'd table
    // entry would silently dispatch the wrong key (e.g. 'back' issuing HOME).
    @Test fun pressKeyDispatchesMappedKeycode() {
        val cases = mapOf(
            "back" to "KEYCODE_BACK",
            "enter" to "KEYCODE_ENTER",
            "up" to "KEYCODE_DPAD_UP",
        )
        for ((key, keycode) in cases) {
            val commands = mutableListOf<List<String>>()
            StubDriverBackend("android") { commands.add(it) }.pressKey(key)
            assertEquals(listOf(listOf("shell", "input", "keyevent", keycode)), commands, key)
        }
    }

    @Test fun pressKeyRejectsUnknownKeyInsteadOfSilentlyDoingNothing() {
        val commands = mutableListOf<List<String>>()
        val backend = StubDriverBackend("android") { commands.add(it) }
        assertFailsWith<IllegalArgumentException> { backend.pressKey("zorp") }
        assertTrue(commands.isEmpty())
    }

    @Test fun inputTextTypesAtCursorWithoutClearing() {
        val commands = mutableListOf<List<String>>()
        val backend = StubDriverBackend("android") { commands.add(it) }

        backend.inputText("Emergency Fund")

        assertEquals(listOf(listOf("shell", "input", "text", "Emergency%sFund")), commands)
    }

    @Test fun eraseTextSendsOneDeleteKeyPerCharacter() {
        val commands = mutableListOf<List<String>>()
        val backend = StubDriverBackend("android") { commands.add(it) }

        backend.eraseText(3)

        assertEquals(
            List(3) { listOf("shell", "input", "keyevent", "KEYCODE_DEL") },
            commands,
        )
    }

    @Test fun escapeForAdbInputTextSubstitutesSpaces() {
        assertEquals("hello%sworld", StubDriverBackend.escapeForAdbInputText("hello world"))
    }

    @Test fun escapeForAdbInputTextEscapesShellMetacharacters() {
        val escaped = StubDriverBackend.escapeForAdbInputText("a&b|c;d\$e`f")
        assertEquals("a\\&b\\|c\\;d\\\$e\\`f", escaped)
    }

    @Test fun escapeForAdbInputTextEscapesQuotesAndBackslash() {
        assertEquals("\\'", StubDriverBackend.escapeForAdbInputText("'"))
        assertEquals("\\\"", StubDriverBackend.escapeForAdbInputText("\""))
        assertEquals("\\\\", StubDriverBackend.escapeForAdbInputText("\\"))
    }

    @Test fun escapeForAdbInputTextLeavesSimpleTextAlone() {
        assertEquals("12.34", StubDriverBackend.escapeForAdbInputText("12.34"))
        assertEquals("Coffee", StubDriverBackend.escapeForAdbInputText("Coffee"))
        assertTrue("-5" == StubDriverBackend.escapeForAdbInputText("-5"))
    }
}
