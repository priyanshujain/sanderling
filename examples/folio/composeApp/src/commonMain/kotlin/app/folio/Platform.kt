package app.folio

expect object Platform {
    fun now(): Long
    fun makeId(): String
}
