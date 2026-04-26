package app.folio.core.platform

expect object Platform {
    fun now(): Long
    fun makeId(): String
}
