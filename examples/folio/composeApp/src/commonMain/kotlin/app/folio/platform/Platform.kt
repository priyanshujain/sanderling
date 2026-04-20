package app.folio.platform

expect object Platform {
    fun now(): Long
    fun makeId(): String
}
