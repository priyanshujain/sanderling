import sqlite3InitModule from '@sqlite.org/sqlite-wasm';

let db = null;
let isPersistent = false;

async function init() {
  const sqlite3 = await sqlite3InitModule({
    print: (msg) => console.log('[sqlite]', msg),
    printErr: (msg) => console.error('[sqlite]', msg),
  });

  try {
    const pool = await sqlite3.installOpfsSAHPoolVfs({
      directory: '/folio-sqlite-pool',
      clearOnInit: false,
      initialCapacity: 6,
    });
    db = new pool.OpfsSAHPoolDb('/folio.sqlite3');
    isPersistent = true;
    console.log('[sqlite] using OPFS SAH pool');
  } catch (sahErr) {
    console.warn('[sqlite] OPFS SAH pool unavailable, falling back to in-memory:', sahErr?.message ?? sahErr);
    db = new sqlite3.oo1.DB(':memory:', 'ct');
    isPersistent = false;
  }
}

function execQuery(sql, params) {
  const rows = db.exec({
    sql,
    bind: params,
    returnValue: 'resultRows',
  });
  return { values: rows };
}

function dispatch(data) {
  switch (data && data.action) {
    case 'exec':
      if (!data.sql) throw new Error('exec: missing sql');
      return execQuery(data.sql, data.params);
    case 'begin_transaction':
      db.exec('BEGIN TRANSACTION;');
      return { values: [] };
    case 'end_transaction':
      db.exec('COMMIT;');
      return { values: [] };
    case 'rollback_transaction':
      db.exec('ROLLBACK;');
      return { values: [] };
    default:
      throw new Error('Unsupported action: ' + (data && data.action));
  }
}

const ready = init();

self.onmessage = (event) => {
  const data = event.data;
  ready
    .then(() => {
      const results = dispatch(data);
      self.postMessage({ id: data.id, results });
    })
    .catch((err) => {
      const message = err && err.message ? String(err.message) : String(err);
      console.error('[sqlite worker] error', message, err);
      self.postMessage({ id: data && data.id, error: { message, name: err && err.name } });
    });
};
