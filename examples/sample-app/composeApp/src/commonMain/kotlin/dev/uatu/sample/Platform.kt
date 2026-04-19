package dev.uatu.sample

expect object Platform {
    fun readFile(name: String): String?
    fun writeFile(name: String, content: String)
    fun now(): Long
    fun makeId(): String
}
