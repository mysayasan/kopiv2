const path = require('path')
const HtmlWebPackPlugin = require('html-webpack-plugin')
const CopyPlugin = require('copy-webpack-plugin')
const fs = require('fs')

const htmlPlugin = new HtmlWebPackPlugin({
  // Cache-busting comes from [contenthash] filenames below (mirrors myseliasan),
  // which also covers runtime-loaded split chunks, so a content change always yields
  // a new URL and browsers never serve a stale chunk.
  title: 'MyPintuSan',
  template: path.resolve(__dirname, 'src', 'index.html'),
  favicon: path.resolve(__dirname, 'src', 'assets', 'favicon.ico')
})

const certPath = path.resolve(__dirname, '../../certs/cert.pem')
const keyPath = path.resolve(__dirname, '../../certs/key.pem')
const hasDevCerts = fs.existsSync(certPath) && fs.existsSync(keyPath)

module.exports = {
  entry: { index: path.resolve(__dirname, 'src', 'index.js') },
  output: {
    path: path.resolve(__dirname, '../../static'),
    publicPath: '/',
    filename: '[name].[contenthash:8].js',
    chunkFilename: '[name].[contenthash:8].js',
    clean: true
  },
  resolve: {
    // '@shared' -> the in-repo shared UI module (frontend/shared/src).
    alias: { '@shared': path.resolve(__dirname, '../../../../frontend/shared/src') },
    // Shared files do bare `import ... from 'react'`; resolve them from THIS app's
    // node_modules so there's a single React copy.
    modules: [path.resolve(__dirname, 'node_modules'), 'node_modules']
  },
  plugins: [
    htmlPlugin,
    // Brand favicon + self-hosted Quicksand, served at /assets/*. The server-rendered
    // federated login page (apis/federated_auth.go) links these directly, so they must
    // exist in static/ even though the SPA imports nothing from here.
    new CopyPlugin({
      patterns: [{ from: path.resolve(__dirname, 'src', 'assets'), to: 'assets' }]
    })
  ],
  module: {
    rules: [
      {
        test: /\.css$/,
        use: ['style-loader', 'css-loader']
      },
      {
        test: /\.js$/,
        exclude: /node_modules/,
        use: {
          loader: 'babel-loader',
          options: {
            presets: ['@babel/preset-env', ['@babel/preset-react', { runtime: 'automatic' }]]
          }
        }
      }
    ]
  },
  optimization: {
    splitChunks: { chunks: 'all' }
  },
  devServer: {
    historyApiFallback: true,
    static: './',
    hot: true,
    port: 4005,
    allowedHosts: 'all',
    proxy: [
      {
        context: ['/api', '/swagger', '/health', '/ready', '/metrics'],
        target: 'https://localhost:3005',
        secure: false,
        changeOrigin: true
      }
    ],
    server: hasDevCerts
      ? {
          type: 'https',
          options: {
            key: fs.readFileSync(keyPath),
            cert: fs.readFileSync(certPath),
            ca: fs.readFileSync(certPath)
          }
        }
      : 'http'
  },
  externals: {
    config: JSON.stringify({
      apiUrl: ''
    })
  }
}
