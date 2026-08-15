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
        for (safe in listOf(
            "demo@folio.app",
            "ledger123",
            "Checking",
            "1e10",
            "0.0000001",
            "42",
            "a".repeat(4096),
        )) {
            assertTrue(
                FAST_INPUT_SAFE.matches(safe),
                "expected fast path for length ${safe.length}",
            )
        }
        val fallback = listOf(
            "Emergency Fund", "🙂🔥💸", "  ", "\t\n", "'; DROP TABLE--",
            "<script>alert(1)</script>", "../../etc/passwd", "%s%n", "",
            "-1", "-rf", // a leading dash could be read as an option by `input text`
        )
        for (text in fallback) {
            assertTrue(
                !FAST_INPUT_SAFE.matches(text),
                "expected driver fallback for: $text",
            )
        }
    }

    // chunkForInput must split long input but never start a chunk with '-',
    // which `input text` would read as an option flag.
    @Test fun chunkForInputSplitsToSizeAndReassembles() {
        val text = "a".repeat(4096)
        val chunks = chunkForInput(text, 512)
        assertEquals(text, chunks.joinToString(""))
        assertTrue(
            chunks.all {
                it.length <= 512
            },
            "no chunk may exceed the size",
        )
        assertTrue(chunks.size >= 8, "4096/512 should be at least 8 chunks")
    }

    @Test fun chunkForInputNeverStartsAChunkWithDash() {
        // a boundary that would fall on '-' is pushed past the dashes
        val chunks = chunkForInput("ab--cd", 2)
        assertEquals("ab--cd", chunks.joinToString(""))
        assertTrue(
            chunks.drop(1).none {
                it.startsWith("-")
            },
            "no later chunk may start with '-'",
        )
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
            assertEquals(
                listOf(listOf("shell", "input", "keyevent", keycode)),
                commands,
                key,
            )
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

        assertEquals(
            listOf(listOf("shell", "input", "text", "Emergency%sFund")),
            commands,
        )
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
        assertEquals(
            "hello%sworld",
            StubDriverBackend.escapeForAdbInputText("hello world"),
        )
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
        assertEquals(
            "Coffee",
            StubDriverBackend.escapeForAdbInputText("Coffee"),
        )
        assertTrue("-5" == StubDriverBackend.escapeForAdbInputText("-5"))
    }

    @Test fun typeChunksSendsAllChunksWhenForegroundStable() {
        val sent = mutableListOf<String>()
        val typed =
            typeChunks(listOf("aaa", "bbb", "cc"), "app.folio", {
                "app.folio"
            }) { sent.add(it) }
        assertEquals(listOf("aaa", "bbb", "cc"), sent)
        assertEquals(8, typed)
    }

    @Test fun typeChunksStopsWhenForegroundLeavesMidType() {
        val sent = mutableListOf<String>()
        var calls = 0
        // Foreground holds before chunk 2, then goes foreign before chunk 3.
        val typed = typeChunks(listOf("aaa", "bbb", "ccc"), "app.folio", {
            calls++
            if (calls == 1) "app.folio" else "com.android.launcher"
        }) { sent.add(it) }
        assertEquals(
            listOf("aaa", "bbb"),
            sent,
            "typing must stop at the chunk after focus left",
        )
        assertEquals(6, typed)
    }

    @Test fun typeChunksAlwaysSendsFirstChunkEvenIfForegroundAlreadyForeign() {
        val sent = mutableListOf<String>()
        val typed =
            typeChunks(listOf("aaa", "bbb"), "app.folio", {
                "com.android.launcher"
            }) { sent.add(it) }
        assertEquals(listOf("aaa"), sent)
        assertEquals(3, typed)
    }

    @Test fun typeChunksWithUnknownOwnerSendsEverything() {
        val sent = mutableListOf<String>()
        typeChunks(listOf("aaa", "bbb"), null, { "anything" }) { sent.add(it) }
        assertEquals(listOf("aaa", "bbb"), sent)
    }

    @Test fun parseResumedPackageReadsEachResumedActivityWording() {
        val cases = mapOf(
            "    topResumedActivity=ActivityRecord{8b u0 app.folio/.MainActivity t42}" to
                "app.folio",
            "  mResumedActivity: ActivityRecord{1c u0 com.example.app/.Home t9}" to
                "com.example.app",
            "  ResumedActivity: ActivityRecord{2d u0 app.folio/com.folio.Detail t9}" to
                "app.folio",
        )
        for ((line, want) in cases) {
            assertEquals(want, parseResumedPackage(line), line)
        }
        assertEquals(
            null,
            parseResumedPackage("  mFocusedApp=null\n  no resumed line here"),
        )
    }

    @Test fun maestroKeyForResolvesAndRejects() {
        assertEquals(maestro.KeyCode.BACK, maestroKeyFor("back"))
        assertEquals(maestro.KeyCode.BACK, maestroKeyFor("BACK"))
        assertEquals(maestro.KeyCode.ENTER, maestroKeyFor("enter"))
        assertFailsWith<IllegalArgumentException> { maestroKeyFor("zorp") }
    }

    // An IME left open hides every app node beneath it from the hierarchy, so a
    // form whose submit button sits under the keyboard becomes unreachable for
    // as long as the fuzzer keeps typing into it. Typing must close the
    // keyboard it raised.
    @Test fun dismissSoftKeyboardClosesAnOpenIme() {
        val commands = mutableListOf<String>()
        dismissSoftKeyboard {
            commands.add(it)
            IME_OPEN_DUMPSYS
        }
        assertEquals(
            listOf("dumpsys input_method", "input keyevent 4"),
            commands,
        )
    }

    // The guard is the dangerous half: BACK is only swallowed by an open IME,
    // so dismissing unconditionally would turn every InputText into a back
    // press and walk the fuzzer straight out of the screen it was filling in.
    @Test fun dismissSoftKeyboardSendsNoBackWhenNoImeIsOpen() {
        val commands = mutableListOf<String>()
        dismissSoftKeyboard {
            commands.add(it)
            IME_CLOSED_DUMPSYS
        }
        assertEquals(listOf("dumpsys input_method"), commands)
    }

    // Typing is not the only thing that raises the keyboard: tapping a field
    // raises it too, and nothing was closing that one. The snapshot the picker
    // chooses from is missing every app node the keyboard covers, so the step
    // spends its budget choosing between the few targets left. Closing it
    // before the tree is read is what gives the step its targets back.
    @Test fun aKeyboardInTheTreeIsClosedBeforeTheTreeIsReturned() {
        var backs = 0
        val reads = mutableListOf<String>()
        val settled = treeWithoutKeyboard(
            IME_TREE,
            IME_PACKAGE,
            dismiss = { backs++ },
            reread = { APP_TREE.also { reads.add(it) } },
            sleep = {},
        )
        assertEquals(APP_TREE, settled)
        assertEquals(1, backs, "one BACK closes the keyboard")
        assertEquals(1, reads.size, "the tree is re-read once it is gone")
    }

    // The guard has to be the tree itself. BACK with no keyboard open
    // navigates out of the screen, so a snapshot that pressed it on every read
    // would walk the fuzzer backwards out of the app a step at a time.
    @Test fun aTreeWithNoKeyboardIsReturnedUntouched() {
        var backs = 0
        var reads = 0
        val settled = treeWithoutKeyboard(
            APP_TREE,
            IME_PACKAGE,
            dismiss = { backs++ },
            reread = {
                reads++
                APP_TREE
            },
            sleep = {},
        )
        assertEquals(APP_TREE, settled)
        assertEquals(0, backs, "no keyboard in the tree means no BACK")
        assertEquals(0, reads, "and no second hierarchy read to pay for")
    }

    // An unknown IME package is the honest "cannot tell", and the safe way to
    // be wrong is to leave the keyboard up rather than press BACK blind.
    @Test fun anUnknownImePackageSendsNoBack() {
        var backs = 0
        assertEquals(
            IME_TREE,
            treeWithoutKeyboard(
                IME_TREE,
                null,
                dismiss = { backs++ },
                reread = { APP_TREE },
                sleep = {},
            ),
        )
        assertEquals(0, backs)
    }

    // A keyboard the app puts straight back gets ONE back press, not one per
    // re-read. The flag behind the older dismissal lags a BACK by up to 0.6s,
    // and a burst of them inside that window is how a dismissal turns into
    // navigation.
    @Test fun aKeyboardThatStaysUpIsNotBackPressedRepeatedly() {
        var backs = 0
        var reads = 0
        val settled = treeWithoutKeyboard(
            IME_TREE,
            IME_PACKAGE,
            dismiss = { backs++ },
            reread = {
                reads++
                IME_TREE
            },
            sleep = {},
        )
        assertEquals(IME_TREE, settled, "the caller still gets a tree")
        assertEquals(1, backs)
        assertTrue(
            reads in 1..KEYBOARD_DISMISS_READS,
            "bounded re-reads, got $reads",
        )
    }

    @Test fun imePackageOfReadsTheComponentAndRejectsNonsense() {
        assertEquals(
            "com.google.android.inputmethod.latin",
            imePackageOf(
                "com.google.android.inputmethod.latin/.LatinIME\n",
            ),
        )
        assertEquals(null, imePackageOf("null"))
        assertEquals(null, imePackageOf(""))
        assertEquals(null, imePackageOf("  \n"))
    }

    @Test fun treeShowsImeMatchesTheImesOwnViewIdsOnly() {
        assertTrue(treeShowsIme(IME_TREE, IME_PACKAGE))
        assertTrue(!treeShowsIme(APP_TREE, IME_PACKAGE))
    }
}

private const val IME_PACKAGE = "com.google.android.inputmethod.latin"

private val APP_TREE =
    """
    {"attributes":{"resource-id":"AddAccountScreen"},"children":[
      {"attributes":{"resource-id":"AccountNameField"},"children":[]},
      {"attributes":{"resource-id":"AddAccountSubmit"},"children":[]}]}
    """.trimIndent()

// The submit control is gone: an open keyboard does not merely cover the node,
// it takes it out of the tree the picker enumerates.
private val IME_TREE =
    """
    {"attributes":{"resource-id":"AddAccountScreen"},"children":[
      {"attributes":{"resource-id":"AccountNameField"},"children":[]},
      {"attributes":{
        "resource-id":"com.google.android.inputmethod.latin:id/keyboard_holder"
      },"children":[]}]}
    """.trimIndent()

private val IME_OPEN_DUMPSYS =
    """
      mCurMethodId=com.google.android.inputmethod.latin/.LatinIME
      mInputShown=true
      mSystemReady=true mInteractive=true
    """.trimIndent()

private val IME_CLOSED_DUMPSYS =
    """
      mCurMethodId=com.google.android.inputmethod.latin/.LatinIME
      mInputShown=false
      mSystemReady=true mInteractive=true
    """.trimIndent()
