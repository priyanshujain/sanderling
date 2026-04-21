package dev.sanderling.sidecar

import org.junit.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull
import kotlin.test.assertTrue

class InputTextClearTest {

    @Test fun buildClearKeyeventsEmptyWhenNothingToDelete() {
        assertEquals(emptyList(), StubDriverBackend.buildClearKeyevents(0))
        assertEquals(emptyList(), StubDriverBackend.buildClearKeyevents(-3))
    }

    @Test fun buildClearKeyeventsPrefixesMoveEndThenOneDelPerChar() {
        val args = StubDriverBackend.buildClearKeyevents(3)
        assertEquals(listOf("shell", "input", "keyevent", "KEYCODE_MOVE_END",
            "KEYCODE_DEL", "KEYCODE_DEL", "KEYCODE_DEL"), args)
    }

    @Test fun buildClearKeyeventsCapsAtMaxClearDeletes() {
        val huge = StubDriverBackend.MAX_CLEAR_DELETES * 10
        val args = StubDriverBackend.buildClearKeyevents(huge)
        val deletes = args.count { it == "KEYCODE_DEL" }
        assertEquals(StubDriverBackend.MAX_CLEAR_DELETES, deletes)
        assertEquals("KEYCODE_MOVE_END", args[3])
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


    @Test fun parsesTextFromFocusedNode() {
        val xml = """
            <hierarchy>
              <node text="ignored" focused="false"/>
              <node resource-id="app:id/email" text="old@value" focused="true"/>
            </hierarchy>
        """.trimIndent()

        assertEquals("old@value", StubDriverBackend.parseFocusedText(xml))
    }

    @Test fun returnsEmptyWhenFocusedNodeHasNoTextAttribute() {
        val xml = """<hierarchy><node focused="true"/></hierarchy>"""

        assertEquals("", StubDriverBackend.parseFocusedText(xml))
    }

    @Test fun returnsNullWhenNoFocusedNode() {
        val xml = """<hierarchy><node text="x" focused="false"/></hierarchy>"""

        assertNull(StubDriverBackend.parseFocusedText(xml))
    }

    @Test fun decodesXmlEntitiesInAttribute() {
        val xml = """<hierarchy><node text="a&amp;b&lt;c&quot;d" focused="true"/></hierarchy>"""

        assertEquals("a&b<c\"d", StubDriverBackend.parseFocusedText(xml))
    }

    @Test fun picksFirstFocusedNodeWhenMultiplePresent() {
        val xml = """
            <hierarchy>
              <node text="first" focused="true"/>
              <node text="second" focused="true"/>
            </hierarchy>
        """.trimIndent()

        assertEquals("first", StubDriverBackend.parseFocusedText(xml))
    }
}
