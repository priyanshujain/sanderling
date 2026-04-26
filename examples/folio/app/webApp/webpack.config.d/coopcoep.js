config.devServer = {
  ...(config.devServer || {}),
  port: process.env.WEBAPP_PORT ? Number(process.env.WEBAPP_PORT) : 8088,
  headers: {
    ...((config.devServer && config.devServer.headers) || {}),
    'Cross-Origin-Opener-Policy': 'same-origin',
    'Cross-Origin-Embedder-Policy': 'require-corp',
    'Cross-Origin-Resource-Policy': 'cross-origin',
  },
};
