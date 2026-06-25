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
  plugins: [
    htmlPlugin,
    new CopyPlugin({
      patterns: [{ from: 'src/assets', to: 'assets' }]
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
        use: ['babel-loader']
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
