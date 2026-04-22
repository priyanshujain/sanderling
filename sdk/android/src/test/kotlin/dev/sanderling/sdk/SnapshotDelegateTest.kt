package dev.sanderling.sdk

import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Test

class SnapshotDelegateTest {

    @After fun tearDown() = Sanderling.stopForTest()

    @Test fun camelToSnakeCaseConvertsKnownNames() {
        assertEquals("logged_in", "loggedIn".camelToSnakeCase())
        assertEquals("total_balance", "totalBalance".camelToSnakeCase())
        assertEquals("txn_form_type", "txnFormType".camelToSnakeCase())
        assertEquals("active_account_id", "activeAccountId".camelToSnakeCase())
        assertEquals("add_account_error", "addAccountError".camelToSnakeCase())
        assertEquals("ledger_balance", "ledgerBalance".camelToSnakeCase())
        assertEquals("ledger_rows", "ledgerRows".camelToSnakeCase())
        assertEquals("auth_status", "authStatus".camelToSnakeCase())
        assertEquals("login_error", "loginError".camelToSnakeCase())
        assertEquals("txn_error", "txnError".camelToSnakeCase())
        assertEquals("account_count", "accountCount".camelToSnakeCase())
        assertEquals("focused_input", "focusedInput".camelToSnakeCase())
        assertEquals("txn_form_account_id", "txnFormAccountId".camelToSnakeCase())
    }

    @Test fun camelToSnakeCaseLeavesAlreadyLowercase() {
        assertEquals("screen", "screen".camelToSnakeCase())
        assertEquals("accounts", "accounts".camelToSnakeCase())
    }

    @Test fun snapshotDelegateRegistersWithDerivedKey() {
        val transport = SocketClientTest.FakeTransport()
        val frameThread = PauserTest.FakeFrameThread()
        val runtime = SanderlingRuntime(
            transport = transport,
            pauser = Pauser(frameThread, pauseTimeoutMillis = 2_000L),
            version = "0.0.1",
            platform = "android",
            appPackage = "com.example.test",
        )
        runtime.start()
        // Inject runtime into Sanderling via reflection so snapshot() can register
        val runtimeField = Sanderling::class.java.getDeclaredField("runtime")
        runtimeField.isAccessible = true
        runtimeField.set(Sanderling, runtime)

        var callCount = 0
        val obj = object {
            val loggedIn by Sanderling.snapshot { callCount++; true }
        }

        val snapshot = runtime.snapshot()
        assertEquals(true, snapshot["logged_in"])
        assertEquals(1, callCount)

        // getValue delegates back to lambda
        callCount = 0
        val value = obj.loggedIn
        assertEquals(true, value)
        assertEquals(1, callCount)

        runtime.stop()
        frameThread.shutdown()
    }

    @Test fun snapshotDelegateGetValueReturnsFreshResult() {
        val transport = SocketClientTest.FakeTransport()
        val frameThread = PauserTest.FakeFrameThread()
        val runtime = SanderlingRuntime(
            transport = transport,
            pauser = Pauser(frameThread, pauseTimeoutMillis = 2_000L),
            version = "0.0.1",
            platform = "android",
            appPackage = "com.example.test",
        )
        runtime.start()
        val runtimeField = Sanderling::class.java.getDeclaredField("runtime")
        runtimeField.isAccessible = true
        runtimeField.set(Sanderling, runtime)

        var counter = 0
        val obj = object {
            val accountCount by Sanderling.snapshot { counter }
        }

        counter = 5
        assertEquals(5, obj.accountCount)
        counter = 10
        assertEquals(10, obj.accountCount)

        runtime.stop()
        frameThread.shutdown()
    }
}
