const path = require('path')
const HtmlWebPackPlugin = require('html-webpack-plugin')
const fs = require('fs')

const htmlPlugin = new HtmlWebPackPlugin({
  hash: true,
  title: 'MyIDSan',
  template: path.resolve(__dirname, 'src', 'index.html')
})

const certPath = path.resolve(__dirname, '../../certs/cert.pem')
const keyPath = path.resolve(__dirname, '../../certs/key.pem')
const hasDevCerts = fs.existsSync(certPath) && fs.existsSync(keyPath)

module.exports = {
  entry: { index: path.resolve(__dirname, 'src', 'index.js') },
  output: {
    path: path.resolve(__dirname, '../../static'),
    publicPath: '/',
    clean: false
  },
  plugins: [htmlPlugin],
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
    port: 4001,
    allowedHosts: 'all',
    proxy: [
      {
        context: ['/api', '/swagger', '/health', '/ready', '/metrics'],
        target: 'https://localhost:3001',
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
