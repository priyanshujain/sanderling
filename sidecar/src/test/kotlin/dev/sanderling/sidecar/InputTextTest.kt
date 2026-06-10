package dev.sanderling.sidecar

import org.junit.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertTrue

class InputTextTest {

    // The fast `adb input text` path only handles short shell-safe ASCII. Common
    // app inputs take it; unicode, injection payloads, whitespace, and
    // overflow-length strings must fall back to the driver, which types them
    // correctly. A regression here would corrupt edge-case input or shell-inject
    // the device.
    @Test fun fastInputPathAcceptsOnlyShellSafeAscii() {
        for (safe in listOf("demo@folio.app", "ledger123", "Checking", "-1", "1e10", "0.0000001")) {
            assertTrue(FAST_INPUT_SAFE.matches(safe), "expected fast path for: $safe")
        }
        val fallback = listOf(
            "Emergency Fund", "🙂🔥💸", "  ", "\t\n", "'; DROP TABLE--",
            "<script>alert(1)</script>", "../../etc/passwd", "%s%n", "", "a".repeat(4096),
        )
        for (text in fallback) {
            assertTrue(!FAST_INPUT_SAFE.matches(text), "expected driver fallback for: $text")
        }
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
