package app.folio

const val DEMO_EMAIL = "demo@ledger.app"
const val DEMO_PASSWORD = "ledger123"

fun checkCredentials(email: String, password: String): Boolean {
    return email.trim().lowercase() == DEMO_EMAIL && password == DEMO_PASSWORD
}
