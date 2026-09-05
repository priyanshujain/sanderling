package dev.sanderling.sidecar

import org.junit.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull

// The tree maestro hands back for a login form: it names both fields and says
// nothing about either being a credential entry, because maestro's mapper does
// not copy the password attribute off the device's XML.
private const val LOGIN_TREE = """
{"attributes":{"bounds":"[0,0,1080,2340]"},"children":[
  {"attributes":{"resource-id":"LoginEmail","class":"android.widget.EditText","bounds":"[51,130][429,202]"},"children":[]},
  {"attributes":{"resource-id":"LoginPassword","class":"android.widget.EditText","bounds":"[51,249][429,321]"},"children":[]},
  {"attributes":{"resource-id":"LoginSubmit","class":"android.widget.Button","bounds":"[51,400][429,470]"},"children":[]}
]}
"""

// The same screen as the device reports it, where the fact still exists.
private const val LOGIN_XML = """<?xml version='1.0' encoding='UTF-8'?>
<hierarchy rotation="0">
  <node index="0" resource-id="" class="android.widget.FrameLayout" password="false" bounds="[0,0,1080,2340]">
    <node index="0" resource-id="LoginEmail" class="android.widget.EditText" password="false" bounds="[51,130][429,202]" />
    <node index="1" resource-id="LoginPassword" class="android.widget.EditText" password="true" bounds="[51,249][429,321]" />
    <node index="2" resource-id="LoginSubmit" class="android.widget.Button" password="false" bounds="[51,400][429,470]" />
  </node>
</hierarchy>
"""

private fun secureOf(tree: String, resourceId: String): Boolean? {
    val field = jacksonTree(tree, "resource-id", resourceId) ?: return null
    val secure = field.get("secure") ?: return null
    return secure.asBoolean()
}

private fun jacksonTree(
    tree: String,
    attribute: String,
    value: String,
): com.fasterxml.jackson.databind.JsonNode? {
    val mapper = com.fasterxml.jackson.module.kotlin.jacksonObjectMapper()
    fun walk(
        node: com.fasterxml.jackson.databind.JsonNode,
    ): com.fasterxml.jackson.databind.JsonNode? {
        if (node.get("attributes")?.get(attribute)?.asText() == value) {
            return node
        }
        node.get("children")?.forEach { child ->
            walk(child)?.let { return it }
        }
        return null
    }
    return walk(mapper.readTree(tree))
}

class SecureFactsTest {

    // Without this, a password field and a search box look identical in the
    // tree, and everything downstream that records a typed value has to treat
    // every field as a credential: the whole run reads "[redacted]".
    @Test fun aPasswordFieldIsToldApartFromTheFieldBesideIt() {
        val annotated = withSecureFacts(LOGIN_TREE) { LOGIN_XML }

        assertEquals(true, secureOf(annotated, "LoginPassword"))
        assertEquals(false, secureOf(annotated, "LoginEmail"))
    }

    // Only what a value can be typed into needs the fact, and stating it on a
    // button would be stating it about something the platform never reported.
    @Test fun aNonEditableNodeIsLeftAlone() {
        assertNull(
            secureOf(
                withSecureFacts(LOGIN_TREE) {
                    LOGIN_XML
                },
                "LoginSubmit",
            ),
        )
    }

    // The Go side calls a node editable off its class or its hint, and a field
    // it will happily type into has to be a field this can speak for.
    @Test fun aFieldNamedByItsHintAloneIsStatedToo() {
        val hinted = """
        {"attributes":{"bounds":"[0,0,1080,2340]"},"children":[
          {"attributes":{"resource-id":"Search","class":"android.view.View","hintText":"Search","bounds":"[10,10,200,50]"},"children":[]}
        ]}
        """
        val xml = """<?xml version='1.0' encoding='UTF-8'?>
        <hierarchy rotation="0">
          <node index="0" resource-id="Search" class="android.view.View" password="false" bounds="[10,10,200,50]" />
        </hierarchy>
        """

        assertEquals(false, secureOf(withSecureFacts(hinted) { xml }, "Search"))
    }

    // Unstated means "may be a credential" downstream. Every way this can fail
    // has to land there rather than on a false "not secure".
    @Test fun aFactThatCannotBeReadIsLeftUnstated() {
        for (xml in listOf<String?>(null, "", "<hierarchy", "<hierarchy/>")) {
            val annotated = withSecureFacts(LOGIN_TREE) { xml }
            assertNull(
                secureOf(annotated, "LoginPassword"),
                "xml ${xml ?: "null"}",
            )
            assertNull(
                secureOf(annotated, "LoginEmail"),
                "xml ${xml ?: "null"}",
            )
        }
    }

    // A wrapper drawn to the same bounds as the untagged field inside it shares
    // the field's empty id and its bounds. The class is what still tells them
    // apart; without it the two collide, the field answers for nothing, and
    // every value typed into an untagged field is redacted.
    @Test fun anUntaggedFieldInsideAWrapperOfItsOwnSizeIsStillStated() {
        val wrapped = """
        {"attributes":{"bounds":"[0,0,1080,2340]"},"children":[
          {"attributes":{"class":"android.view.View","bounds":"[51,249][429,321]"},"children":[
            {"attributes":{"class":"android.widget.EditText","bounds":"[51,249][429,321]"},"children":[]}
          ]}
        ]}
        """
        val xml = """<?xml version='1.0' encoding='UTF-8'?>
        <hierarchy rotation="0">
          <node index="0" resource-id="" class="android.view.View" password="false" bounds="[51,249][429,321]">
            <node index="0" resource-id="" class="android.widget.EditText" password="true" bounds="[51,249][429,321]" />
          </node>
        </hierarchy>
        """

        val field = jacksonTree(
            withSecureFacts(wrapped) { xml },
            "class",
            "android.widget.EditText",
        )

        assertEquals(true, field?.get("secure")?.asBoolean())
    }

    // Two nodes sharing a key answer for neither: taking the first would state
    // "not secure" about a field that may be the other one.
    @Test fun anAmbiguousMatchIsLeftUnstated() {
        val duplicated = """<?xml version='1.0' encoding='UTF-8'?>
        <hierarchy rotation="0">
          <node index="0" resource-id="LoginPassword" class="android.widget.EditText" password="false" bounds="[51,249][429,321]" />
          <node index="1" resource-id="LoginPassword" class="android.widget.EditText" password="true" bounds="[51,249][429,321]" />
        </hierarchy>
        """
        assertNull(
            secureOf(
                withSecureFacts(LOGIN_TREE) {
                    duplicated
                },
                "LoginPassword",
            ),
        )
    }

    // A screen with nothing to type into must not pay a device read for an
    // answer no field is waiting on.
    @Test fun aScreenWithNoTextFieldNeverReadsTheDevice() {
        val listTree =
            """{"attributes":{"resource-id":"HomeScreen"},"children":[]}"""
        var reads = 0

        val annotated = withSecureFacts(listTree) {
            reads++
            LOGIN_XML
        }

        assertEquals(0, reads)
        assertEquals(listTree, annotated)
    }

    // The fact is read through a call maestro keeps private, so a maestro
    // upgrade that renames it would leave every typed value redacted again with
    // nothing failing. This fails the build instead.
    @Test fun maestroStillExposesTheCallTheDeviceXmlComesFrom() {
        val call = maestro.drivers.AndroidDriver::class.java
            .getDeclaredMethod(
                "callViewHierarchy",
                Int::class.javaPrimitiveType,
            )

        assertEquals(
            String::class.java,
            call.returnType.getMethod("getHierarchy").returnType,
        )
    }

    // The rest of the tree has to survive the annotation: it is the same tree
    // every selector, bounds read and screen classification runs against.
    @Test fun theTreeIsOtherwiseUnchanged() {
        val mapper = com.fasterxml.jackson.module.kotlin.jacksonObjectMapper()
        val annotated = mapper.readTree(
            withSecureFacts(LOGIN_TREE) {
                LOGIN_XML
            },
        )

        val stated = annotated.findParents("secure")
        assertEquals(2, stated.size)
        for (node in stated) {
            (node as com.fasterxml.jackson.databind.node.ObjectNode)
                .remove("secure")
        }
        assertEquals(mapper.readTree(LOGIN_TREE), annotated)
    }
}
