const path = require('path')
const HtmlWebPackPlugin = require('html-webpack-plugin')
const fs = require('fs')
const htmlPlugin = new HtmlWebPackPlugin({
  // Cache-busting comes from [contenthash] filenames below, which also covers
  // runtime-loaded split chunks, so a content change always yields a new URL and
  // browsers never serve a stale chunk.
  title: 'MySeliaSan',
  template: path.resolve(__dirname, 'src', 'index.html'),
  favicon: './src/assets/favicon.ico'
})

const CopyPlugin = require('copy-webpack-plugin')

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
    // '@mymatasan' -> the mymatasan app's view source, so myseliasan can embed the node's
    // real pages/CSS (styled in an isolated Shadow DOM) and follow mymatasan design changes.
    alias: {
      '@shared': path.resolve(__dirname, '../../../../frontend/shared/src'),
      '@mymatasan': path.resolve(__dirname, '../../../mymatasan/views/react-webpack/src/views'),
    },
    // Shared files do bare `import ... from 'react'`; resolve them from THIS app's
    // node_modules so there's a single React copy.
    modules: [path.resolve(__dirname, 'node_modules'), 'node_modules']
  },
  plugins: [
    htmlPlugin,
    new CopyPlugin({
      patterns: [{ from: 'src/assets', to: 'assets' }]
    })
  ],
  module: {
    rules: [
      {
        // CSS imported with `?raw` is returned as a plain string (not injected into the
        // document) so it can be injected into a Shadow DOM — this is how the embedded
        // mymatasan node pages get mymatasan's real, isolated styling.
        test: /\.css$/,
        resourceQuery: /raw/,
        type: 'asset/source'
      },
      {
        test: /\.css$/,
        resourceQuery: { not: [/raw/] },
        use: ['style-loader', 'css-loader']
      },
      {
        // Inline presets (instead of relying only on .babelrc) so files OUTSIDE this
        // app — i.e. the @shared module under frontend/shared/src — are also transpiled.
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
    port: 4001,
    allowedHosts: 'all',
    server: {
      type: 'https',
      options: {
        key: fs.readFileSync('../../certs/key.pem'),
        cert: fs.readFileSync('../../certs/cert.pem'),
        ca: fs.readFileSync('../../certs/cert.pem')
      }
    }
  },
  externals: {
    config: JSON.stringify({ apiUrl: 'https://localhost:3002' })
  }
}
