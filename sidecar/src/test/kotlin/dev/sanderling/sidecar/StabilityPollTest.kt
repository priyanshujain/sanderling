package dev.sanderling.sidecar

import org.junit.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class StabilityPollTest {
    @Test fun returnsAfterTwoIdenticalSnapshots() {
        val snapshots = mutableListOf<String>()
        val start = System.currentTimeMillis()
        pollUntilStable(800L) {
            val value = "stable"
            snapshots.add(value)
            value
        }
        val elapsed = System.currentTimeMillis() - start
        assertTrue(elapsed < 600L, "should settle within a couple poll intervals, took ${elapsed}ms")
        assertTrue(snapshots.size >= 2, "must observe at least two snapshots to confirm stability")
    }

    @Test fun returnsAfterChangingThenStabilizing() {
        var calls = 0
        pollUntilStable(2000L) {
            calls++
            if (calls <= 2) "frame-$calls" else "stable"
        }
        // After 2 changing frames, calls 3 and 4 are both "stable" -> exit.
        assertTrue(calls >= 4, "expected at least 4 snapshots, got $calls")
    }

    @Test fun stopsAtDeadlineWhenNeverStable() {
        var calls = 0
        val budget = 240L
        val start = System.currentTimeMillis()
        pollUntilStable(budget) {
            calls++
            "frame-$calls"
        }
        val elapsed = System.currentTimeMillis() - start
        assertTrue(elapsed in budget..(budget + 200L), "expected to hit cap, elapsed=$elapsed")
    }

    @Test fun zeroBudgetReturnsImmediately() {
        var calls = 0
        pollUntilStable(0L) {
            calls++
            "x"
        }
        assertEquals(0, calls)
    }

    @Test fun structuralHashIgnoresBoundsAndIdenticalForSemanticallyEqualTrees() {
        val a = """
        {"attributes":{"resource-id":"LoginScreen","bounds":"[0,0,1080,2340]"},
         "children":[
           {"attributes":{"resource-id":"LoginEmail","bounds":"[10,10,1070,100]","text":"a@b"},"children":[]}
         ]}
        """.trimIndent()
        val b = """
        {"attributes":{"resource-id":"LoginScreen","bounds":"[0,0,1080,2342]"},
         "children":[
           {"attributes":{"resource-id":"LoginEmail","bounds":"[10,11,1070,101]","text":"a@b"},"children":[]}
         ]}
        """.trimIndent()
        assertEquals(structuralHash(a), structuralHash(b), "bounds-only flicker must not change hash")
    }

    @Test fun structuralHashDiffersWhenContentChanges() {
        val a = """{"attributes":{"resource-id":"LoginEmail","text":"a@b"},"children":[]}"""
        val b = """{"attributes":{"resource-id":"LoginEmail","text":"c@d"},"children":[]}"""
        assertTrue(structuralHash(a) != structuralHash(b), "text change must alter hash")
    }
}
