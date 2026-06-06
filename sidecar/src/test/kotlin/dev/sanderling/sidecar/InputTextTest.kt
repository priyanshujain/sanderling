package dev.sanderling.sidecar

import org.junit.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertTrue

class InputTextTest {

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
