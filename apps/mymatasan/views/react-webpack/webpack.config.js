const path = require('path')
const HtmlWebPackPlugin = require('html-webpack-plugin')
const fs = require('fs')
const htmlPlugin = new HtmlWebPackPlugin({
  hash: true,
  title: 'MyMataSan',
  template: path.resolve(__dirname, 'src', 'index.html'),
  favicon: './src/assets/favicon.ico'
})

const CopyPlugin = require('copy-webpack-plugin')

module.exports = {
  entry: { index: path.resolve(__dirname, 'src', 'index.js') },
  output: {
    path: path.resolve(__dirname, '../../static'),
    publicPath: '/'
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
        test: /\.(s*)css$/,
        use: ['style-loader', 'css-loader', 'sass-loader']
      },
      {
        test: /\.js$/,
        exclude: /node_modules/,
        use: ['babel-loader']
      },
      {
        test: /\.(svg|png|jpe?g|gif)$/,
        loader: 'url-loader'
      },
      {
        test: /\.(woff(2)?|ttf|eot|svg|png|jpe?g|gif|ico|ogv)(\?v=\d+\.\d+\.\d+)?$/,
        use: [
          {
            loader: 'file-loader',
            options: {
              name: '[name].[ext]',
              outputPath: 'assets/'
            }
          }
        ]
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
    port: 4000,
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
    config: JSON.stringify(
      process.env.NODE_ENV === 'dev'
        ? {
            apiUrl: 'http://localhost:3000'
          }
        : {
            apiUrl: 'http://localhost:3000'
          }
    )
  }
}
