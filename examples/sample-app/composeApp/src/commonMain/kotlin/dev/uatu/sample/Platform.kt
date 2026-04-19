package dev.uatu.sample

expect object Platform {
    fun now(): Long
    fun makeId(): String
}
