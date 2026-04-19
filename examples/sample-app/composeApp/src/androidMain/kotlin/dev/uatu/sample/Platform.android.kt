package dev.uatu.sample

import android.content.Context
import java.io.File
import java.util.UUID

actual object Platform {
    @Volatile private var appContext: Context? = null

    fun init(context: Context) {
        appContext = context.applicationContext
    }

    private fun dir(): File {
        val ctx = requireNotNull(appContext) { "Platform.init() must be called from Application.onCreate()" }
        return File(ctx.filesDir, "ledger").apply { mkdirs() }
    }

    actual fun readFile(name: String): String? {
        val f = File(dir(), name)
        return if (f.exists()) f.readText() else null
    }

    actual fun writeFile(name: String, content: String) {
        val f = File(dir(), name)
        val tmp = File(dir(), "$name.tmp")
        tmp.writeText(content)
        if (!tmp.renameTo(f)) {
            f.writeText(content)
            tmp.delete()
        }
    }

    actual fun now(): Long = System.currentTimeMillis()

    actual fun makeId(): String = UUID.randomUUID().toString()
}
