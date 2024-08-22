const path = require('path')
const HtmlWebPackPlugin = require('html-webpack-plugin')
// const WorkboxPlugin = require('workbox-webpack-plugin')
const fs = require('fs')
// const Dotenv = require('dotenv-webpack')
const htmlPlugin = new HtmlWebPackPlugin({
  hash: true,
  title: 'MyMataSan',
  template: path.resolve(__dirname, 'src', 'index.html'),
  // template: "./src/index.html",
  // filename: "./index.html",
  favicon: './src/assets/favicon.ico'
})

// const workboxPlugin = new WorkboxPlugin.GenerateSW({
//   // these options encourage the ServiceWorkers to get in there fast
//   // and not allow any straggling "old" SWs to hang around
//   clientsClaim: true,
//   skipWaiting: true,
//   maximumFileSizeToCacheInBytes: 100000000
// })

const CopyPlugin = require('copy-webpack-plugin')

module.exports = {
  entry: { index: path.resolve(__dirname, 'src', 'index.js') },
  // entry: './src/index.js',
  output: {
    path: path.resolve(__dirname, '../../static'),
    publicPath: '/'
  },
  plugins: [
    htmlPlugin,
    // new Dotenv({
    //   path: './.env', // Path to .env file (this is the default)
    //   safe: false // load .env.example (defaults to "false" which does not use dotenv-safe)
    // }),
    //workboxPlugin,
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
        // requestCert: true,
      }
    }
  },
  externals: {
    config: JSON.stringify(
      process.env.NODE_ENV === 'dev'
        ? {
            apiUrl: 'https://localhost:3000'
          }
        : {
            apiUrl: 'https://localhost:3000'
          }
    )
  }
}
