package app.folio

import android.app.Application
import app.folio.data.AndroidLedgerContext
import app.folio.data.Repository
import app.folio.sanderling.AccountSnapshots
import app.folio.sanderling.AuthSnapshots
import app.folio.sanderling.LedgerSnapshots
import app.folio.sanderling.NavigationSnapshots
import dev.sanderling.sdk.Sanderling

class FolioApplication : Application() {
    override fun onCreate() {
        super.onCreate()
        AndroidLedgerContext.context = applicationContext
        Repository.init()
        Sanderling.start(this)
        AuthSnapshots
        AccountSnapshots
        LedgerSnapshots
        NavigationSnapshots
    }
}
