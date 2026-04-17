package dev.uatu.sdk

import java.io.DataInputStream
import java.io.DataOutputStream
import java.io.IOException
import java.io.InputStream
import java.io.OutputStream
import org.json.JSONArray
import org.json.JSONObject

enum class MessageType(val wire: String) {
    HELLO("HELLO"),
    PAUSE("PAUSE"),
    RESUME("RESUME"),
    STATE("STATE"),
    EXTRACT_RESULT("EXTRACT_RESULT"),
    GOODBYE("GOODBYE");

    companion object {
        fun fromWire(wire: String): MessageType =
            values().firstOrNull { it.wire == wire }
                ?: throw IOException("unknown message type: $wire")
    }
}

data class Message(
    val type: MessageType,
    val id: Long = 0L,
    val version: String? = null,
    val platform: String? = null,
    val appPackage: String? = null,
    val snapshots: Map<String, Any?>? = null,
    val extractor: String? = null,
    val result: Any? = null,
    val error: String? = null,
    val reason: String? = null,
) {
    companion object {
        fun hello(version: String, platform: String, appPackage: String): Message =
            Message(MessageType.HELLO, version = version, platform = platform, appPackage = appPackage)

        fun pause(id: Long): Message = Message(MessageType.PAUSE, id = id)

        fun resume(id: Long): Message = Message(MessageType.RESUME, id = id)

        fun state(id: Long, snapshots: Map<String, Any?>): Message =
            Message(MessageType.STATE, id = id, snapshots = snapshots)

        fun extractResult(id: Long, extractor: String, result: Any?, error: String? = null): Message =
            Message(MessageType.EXTRACT_RESULT, id = id, extractor = extractor, result = result, error = error)

        fun goodbye(reason: String): Message = Message(MessageType.GOODBYE, reason = reason)
    }
}

object Protocol {
    const val MAX_FRAME_SIZE: Int = 16 * 1024 * 1024

    @Throws(IOException::class)
    fun write(output: OutputStream, message: Message) {
        val bytes = toJson(message).toString().toByteArray(Charsets.UTF_8)
        if (bytes.size > MAX_FRAME_SIZE) {
            throw IOException("frame of ${bytes.size} bytes exceeds maximum $MAX_FRAME_SIZE")
        }
        DataOutputStream(output).apply {
            writeInt(bytes.size)
            write(bytes)
            flush()
        }
    }

    @Throws(IOException::class)
    fun read(input: InputStream): Message {
        val dataInput = DataInputStream(input)
        val length = dataInput.readInt()
        if (length < 0 || length > MAX_FRAME_SIZE) {
            throw IOException("frame of $length bytes exceeds maximum $MAX_FRAME_SIZE")
        }
        val bytes = ByteArray(length)
        dataInput.readFully(bytes)
        return fromJson(JSONObject(String(bytes, Charsets.UTF_8)))
    }

    private fun toJson(message: Message): JSONObject {
        val json = JSONObject()
        json.put("type", message.type.wire)
        if (message.id != 0L) json.put("id", message.id)
        message.version?.let { json.put("version", it) }
        message.platform?.let { json.put("platform", it) }
        message.appPackage?.let { json.put("app_package", it) }
        message.snapshots?.let { snapshots ->
            val snapshotsJson = JSONObject()
            for ((key, value) in snapshots) {
                snapshotsJson.put(key, wrap(value))
            }
            json.put("snapshots", snapshotsJson)
        }
        message.extractor?.let { json.put("extractor", it) }
        message.result?.let { json.put("result", wrap(it)) }
        message.error?.let { json.put("error", it) }
        message.reason?.let { json.put("reason", it) }
        return json
    }

    private fun fromJson(json: JSONObject): Message {
        val typeString = json.optString("type", "")
        if (typeString.isEmpty()) throw IOException("missing type")
        return Message(
            type = MessageType.fromWire(typeString),
            id = json.optLong("id", 0L),
            version = json.optStringOrNull("version"),
            platform = json.optStringOrNull("platform"),
            appPackage = json.optStringOrNull("app_package"),
            snapshots = json.optJSONObject("snapshots")?.let { snapshotsJson ->
                snapshotsJson.keys().asSequence().associateWith { unwrap(snapshotsJson.get(it)) }
            },
            extractor = json.optStringOrNull("extractor"),
            result = if (json.has("result") && !json.isNull("result")) unwrap(json.get("result")) else null,
            error = json.optStringOrNull("error"),
            reason = json.optStringOrNull("reason"),
        )
    }

    private fun wrap(value: Any?): Any = when (value) {
        null -> JSONObject.NULL
        is Number, is Boolean, is String -> value
        is Map<*, *> -> JSONObject().also { json ->
            for ((key, nested) in value) json.put(key.toString(), wrap(nested))
        }
        is List<*> -> JSONArray().also { array ->
            for (item in value) array.put(wrap(item))
        }
        else -> value.toString()
    }

    private fun unwrap(value: Any?): Any? = when (value) {
        JSONObject.NULL, null -> null
        is JSONObject -> value.keys().asSequence().associateWith { unwrap(value.get(it)) }
        is JSONArray -> buildList { for (index in 0 until value.length()) add(unwrap(value.get(index))) }
        else -> value
    }

    private fun JSONObject.optStringOrNull(key: String): String? =
        if (has(key) && !isNull(key)) getString(key) else null
}
