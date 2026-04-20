package dev.uatu.sidecar

import org.junit.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull

class InputTextClearTest {

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
