package dev.sanderling.sidecar

import hierarchy.AXElement
import hierarchy.AXFrame
import org.junit.Test
import kotlin.test.assertEquals

class IosHierarchyTest {

    private fun element(
        elementType: Int,
        label: String = "",
        identifier: String = "",
        title: String = "",
        value: String = "",
        placeholderValue: String = "",
        children: ArrayList<AXElement> = arrayListOf(),
    ) = AXElement(
        label,
        elementType,
        identifier,
        0,
        0L,
        0,
        false,
        0,
        false,
        placeholderValue,
        value,
        AXFrame(10f, 20f, 100f, 50f),
        true,
        title,
        children,
    )

    @Test
    fun buttonIsClickableNotEditable() {
        val node = iosAxElementToTreeNode(element(elementType = 9, label = "Sign in", identifier = "LoginSubmit"))
        assertEquals(true, node["clickable"])
        assertEquals(false, node["editable"])
        @Suppress("UNCHECKED_CAST")
        val attributes = node["attributes"] as Map<String, String>
        assertEquals("Button", attributes["class"])
        assertEquals("LoginSubmit", attributes["resource-id"])
        assertEquals("Sign in", attributes["accessibilityText"])
    }

    @Test
    fun textFieldIsEditableNotClickable() {
        val node = iosAxElementToTreeNode(element(elementType = 49, value = "demo@folio.app"))
        assertEquals(false, node["clickable"])
        assertEquals(true, node["editable"])
        @Suppress("UNCHECKED_CAST")
        val attributes = node["attributes"] as Map<String, String>
        assertEquals("TextField", attributes["class"])
        assertEquals("demo@folio.app", attributes["text"])
        assertEquals("demo@folio.app", attributes["value"])
    }

    @Test
    fun titleWinsOverValueForText() {
        val node = iosAxElementToTreeNode(element(elementType = 48, title = "Heading", value = "ignored"))
        @Suppress("UNCHECKED_CAST")
        val attributes = node["attributes"] as Map<String, String>
        assertEquals("Heading", attributes["text"])
        assertEquals("StaticText", attributes["class"])
    }

    @Test
    fun switchOnIsChecked() {
        val node = iosAxElementToTreeNode(element(elementType = 40, value = "1"))
        assertEquals(true, node["checked"])
        assertEquals(true, node["clickable"])
    }

    @Test
    fun scrollViewIsScrollable() {
        val node = iosAxElementToTreeNode(element(elementType = 46))
        @Suppress("UNCHECKED_CAST")
        val attributes = node["attributes"] as Map<String, String>
        assertEquals("true", attributes["scrollable"])
        assertEquals("ScrollView", attributes["class"])
    }

    @Test
    fun childrenAreMappedRecursively() {
        val child = element(elementType = 48, title = "Inner")
        val node = iosAxElementToTreeNode(element(elementType = 1, children = arrayListOf(child)))
        @Suppress("UNCHECKED_CAST")
        val children = node["children"] as List<Map<String, Any?>>
        assertEquals(1, children.size)
        @Suppress("UNCHECKED_CAST")
        val childAttributes = children[0]["attributes"] as Map<String, String>
        assertEquals("Inner", childAttributes["text"])
    }

    @Test
    fun unknownElementTypeFallsBackToOther() {
        assertEquals("Other", iosElementTypeName(999))
    }
}
