package dev.sanderling.sidecar

import org.junit.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class EraseTextTest {

    // maestro's eraseText sends one delete per character through its
    // instrumentation, measured at 29.6 ms/char on the API 34 emulator: the
    // 4096-character string the corpus types cost ~121s to clear, a fifth of a
    // 20 minute run for one step. Selecting the field and deleting the
    // selection is the same two key events whatever the field holds.
    @Test fun aClearedFieldCostsTwoKeyEventsWhateverItsLength() {
        for (length in listOf(1, 21, 512, 4096)) {
            val sent = mutableListOf<String>()
            eraseFocusedField(length, { sent.add(it) }) { 0 }
            assertEquals(
                listOf(SELECT_ALL_COMMAND, DELETE_KEY_COMMAND),
                sent,
                "length $length must not scale the erase",
            )
        }
    }

    // The dangerous failure is a fast erase that leaves characters behind: the
    // next InputText appends to the residue and every reading downstream is
    // wrong with nothing to catch it. A field the select-all did not clear is
    // finished off per character rather than assumed empty.
    @Test fun aFieldTheSelectAllMissedIsFinishedOffPerCharacter() {
        val sent = mutableListOf<String>()
        eraseFocusedField(4096, { sent.add(it) }) { 4096 }

        assertEquals(SELECT_ALL_COMMAND, sent.first())
        assertEquals(DELETE_KEY_COMMAND, sent[1])
        assertEquals(
            4096,
            sent.drop(2).sumOf { command ->
                command.removePrefix("input keyevent ").split(" ").size
            },
            "every character must still be deleted",
        )
    }

    // Unknown is not empty. A tree that cannot name the focused field is no
    // evidence the erase worked, and the safe way to be wrong is the delete
    // that costs time rather than the one that leaves residue.
    @Test fun aFieldThatCannotBeReadIsFinishedOffRatherThanAssumedEmpty() {
        val sent = mutableListOf<String>()
        eraseFocusedField(8, { sent.add(it) }) { null }
        assertTrue(sent.size > 2, "an unverified erase must not stop at two")
    }

    @Test fun nothingToEraseIssuesNoKeysAtAll() {
        val sent = mutableListOf<String>()
        eraseFocusedField(0, { sent.add(it) }) { 0 }
        eraseFocusedField(-1, { sent.add(it) }) { 0 }
        assertEquals(emptyList(), sent)
    }

    // Batching is what keeps the fallback affordable: one round trip per batch
    // rather than one per character, measured 2.3 ms/char against maestro's
    // 29.6. The count must survive the batching exactly.
    @Test fun deleteKeyCommandsBatchesWithoutLosingACharacter() {
        for (count in listOf(1, 199, 200, 201, 4096)) {
            val commands = deleteKeyCommands(count, DELETE_BATCH_KEYS)
            val keys = commands.flatMap {
                it.removePrefix("input keyevent ").split(" ")
            }
            assertEquals(count, keys.size, "count $count")
            assertTrue(keys.all { it == "67" }, "only KEYCODE_DEL")
            assertTrue(
                commands.size <= (count + DELETE_BATCH_KEYS - 1) /
                    DELETE_BATCH_KEYS,
                "count $count used ${commands.size} round trips",
            )
        }
    }

    // The erase targets the field the runner just tapped, so the length that
    // decides whether it worked is the focused editable node's, not some other
    // field that legitimately still holds text.
    @Test fun focusedEditableTextLengthReadsTheFocusedFieldOnly() {
        assertEquals(4096, focusedEditableTextLength(TREE_WITH_FULL_FIELD))
        assertEquals(0, focusedEditableTextLength(TREE_WITH_EMPTY_FIELD))
    }

    @Test fun aTreeWithNoFocusedFieldReadsAsUnknown() {
        assertEquals(null, focusedEditableTextLength(TREE_WITH_NO_FOCUS))
        assertEquals(null, focusedEditableTextLength(""))
        assertEquals(null, focusedEditableTextLength("not json"))
    }
}

private fun tree(focusedText: String?, otherText: String): String {
    val focused = focusedText?.let {
        """{"attributes":{"resource-id":"AccountNameField",
           "editable":"true","focused":"true","text":"$it"},"children":[]},"""
    } ?: ""
    return """
    {"attributes":{"resource-id":"AddAccountScreen"},"children":[
      $focused
      {"attributes":{"resource-id":"OtherField","editable":"true",
       "focused":"false","text":"$otherText"},"children":[]}]}
    """.trimIndent()
}

private val TREE_WITH_FULL_FIELD = tree("a".repeat(4096), "keep me")
private val TREE_WITH_EMPTY_FIELD = tree("", "keep me")
private val TREE_WITH_NO_FOCUS = tree(null, "keep me")
