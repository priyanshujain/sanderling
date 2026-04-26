// Webpack picks up `new Worker(new URL("./sqlite.worker.js", import.meta.url),
// { type: "module" })` from the compiled Kotlin/wasm output and bundles the
// worker. The bundled worker imports `@sqlite.org/sqlite-wasm`, which in turn
// loads sqlite3.wasm via `new URL("sqlite3.wasm", import.meta.url)`. Enabling
// asyncWebAssembly lets webpack process that .wasm reference correctly.
config.experiments = {
  ...(config.experiments || {}),
  asyncWebAssembly: true,
};
