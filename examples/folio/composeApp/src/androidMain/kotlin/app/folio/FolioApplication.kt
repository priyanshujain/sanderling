package app.folio

import android.app.Application
import app.folio.data.AndroidLedgerContext
import app.folio.data.Repository

class FolioApplication : Application() {
    override fun onCreate() {
        super.onCreate()
        AndroidLedgerContext.context = applicationContext
        Repository.init()
    }
}
