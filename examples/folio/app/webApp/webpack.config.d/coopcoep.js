config.devServer = {
  ...(config.devServer || {}),
  headers: {
    ...((config.devServer && config.devServer.headers) || {}),
    'Cross-Origin-Opener-Policy': 'same-origin',
    'Cross-Origin-Embedder-Policy': 'require-corp',
    'Cross-Origin-Resource-Policy': 'cross-origin',
  },
};
